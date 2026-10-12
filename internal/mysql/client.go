package mysql

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/ng-life/mysql-explorer/internal/config"
)

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Key      string `json:"key"`
}
type Query struct {
	Filter map[string]any `json:"filter"`
	Sort   map[string]int `json:"sort"`
	Limit  int            `json:"limit"`
	Skip   int            `json:"skip"`
}
type Result struct {
	Documents []map[string]any `json:"documents"`
	Columns   []Column         `json:"columns"`
	HasMore   bool             `json:"has_more"`
}
type Reader interface {
	Databases(context.Context) ([]string, error)
	Tables(context.Context, string) ([]string, error)
	Find(context.Context, string, string, Query) (Result, error)
}
type Client struct{ DB *sql.DB }
type Pool struct {
	mu       sync.Mutex
	clusters []config.Cluster
	clients  map[int]*Client
}

func NewPool(clusters []config.Cluster) *Pool {
	return &Pool{clusters: clusters, clients: map[int]*Client{}}
}
func (p *Pool) Get(ctx context.Context, id int) (Reader, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.clients[id]; c != nil {
		return c, nil
	}
	if id < 0 || id >= len(p.clusters) {
		return nil, fmt.Errorf("unknown service")
	}
	cl := p.clusters[id]
	cfg := driver.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr = cl.User, cl.Password, "tcp", cl.Address
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 12*time.Second, 12*time.Second
	// No multi-statements, local files or arbitrary SQL are exposed by this application.
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	c := &Client{DB: db}
	p.clients[id] = c
	return c, nil
}
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.clients {
		c.DB.Close()
	}
}
func list(ctx context.Context, db *sql.DB, statement string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
func (c *Client) Databases(ctx context.Context) ([]string, error) {
	return list(ctx, c.DB, "SHOW DATABASES")
}
func (c *Client) Tables(ctx context.Context, db string) ([]string, error) {
	return list(ctx, c.DB, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", db)
}
func (c *Client) columns(ctx context.Context, db, table string) ([]Column, error) {
	rows, err := c.DB.QueryContext(ctx, "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", db, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Column{}
	for rows.Next() {
		var col Column
		var nullable string
		if err := rows.Scan(&col.Name, &col.Type, &nullable, &col.Key); err != nil {
			return nil, err
		}
		col.Nullable = nullable == "YES"
		out = append(out, col)
	}
	return out, rows.Err()
}

// quoteIdentifier handles MySQL identifiers independently from parameter values.
func quoteIdentifier(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
func buildQuery(db, table string, cols []Column, q Query) (string, []any, error) {
	if q.Limit < 1 || q.Limit > 200 || q.Skip < 0 || q.Skip > 1000000 {
		return "", nil, fmt.Errorf("invalid pagination")
	}
	allowed := map[string]bool{}
	for _, col := range cols {
		allowed[col.Name] = true
	}
	keys := make([]string, 0, len(q.Filter))
	for key := range q.Filter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []any{}
	where := []string{}
	for _, key := range keys {
		if !allowed[key] {
			return "", nil, fmt.Errorf("unknown filter column")
		}
		value := q.Filter[key]
		if value == nil {
			where = append(where, quoteIdentifier(key)+" IS NULL")
			continue
		}
		switch v := value.(type) {
		case string, bool, float64:
			args = append(args, value)
		case json.Number:
			args = append(args, v.String())
		default:
			return "", nil, fmt.Errorf("filter values must be scalars")
		}
		where = append(where, quoteIdentifier(key)+" = ?")
	}
	statement := "SELECT * FROM " + quoteIdentifier(db) + "." + quoteIdentifier(table)
	if len(where) > 0 {
		statement += " WHERE " + strings.Join(where, " AND ")
	}
	keys = keys[:0]
	for key := range q.Sort {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	order := []string{}
	for _, key := range keys {
		if !allowed[key] || (q.Sort[key] != 1 && q.Sort[key] != -1) {
			return "", nil, fmt.Errorf("invalid sort")
		}
		direction := " ASC"
		if q.Sort[key] == -1 {
			direction = " DESC"
		}
		order = append(order, quoteIdentifier(key)+direction)
	}
	// Primary key tie breakers make pagination stable while data is unchanged.
	for _, col := range cols {
		if col.Key == "PRI" {
			if _, exists := q.Sort[col.Name]; !exists {
				order = append(order, quoteIdentifier(col.Name)+" ASC")
			}
		}
	}
	if len(order) > 0 {
		statement += " ORDER BY " + strings.Join(order, ", ")
	}
	statement += " LIMIT ? OFFSET ?"
	args = append(args, q.Limit+1, q.Skip)
	return statement, args, nil
}
func (c *Client) Find(ctx context.Context, db, table string, q Query) (Result, error) {
	out := Result{Documents: []map[string]any{}}
	cols, err := c.columns(ctx, db, table)
	if err != nil {
		return out, err
	}
	if len(cols) == 0 {
		return out, fmt.Errorf("table unavailable")
	}
	out.Columns = cols
	statement, args, err := buildQuery(db, table, cols, q)
	if err != nil {
		return out, err
	}
	// A read-only transaction adds a server-side guard around generated SELECTs.
	tx, err := c.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return out, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return out, err
	}
	size := 0
	for rows.Next() {
		if len(out.Documents) == q.Limit {
			out.HasMore = true
			break
		}
		values := make([]any, len(names))
		dest := make([]any, len(names))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return out, err
		}
		doc := map[string]any{}
		for i, name := range names {
			switch v := values[i].(type) {
			case []byte:
				typ := types[i].DatabaseTypeName()
				if strings.Contains(typ, "BLOB") || typ == "BINARY" || typ == "VARBINARY" || typ == "BIT" || typ == "GEOMETRY" {
					doc[name] = map[string]string{"base64": base64.StdEncoding.EncodeToString(v)}
				} else {
					doc[name] = string(v)
				}
			case int64, uint64:
				doc[name] = fmt.Sprint(v)
			default:
				doc[name] = v
			}
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			return out, err
		}
		size += len(raw)
		if size > 32*1024*1024 {
			return out, fmt.Errorf("result too large")
		}
		out.Documents = append(out.Documents, doc)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	return out, nil
}
