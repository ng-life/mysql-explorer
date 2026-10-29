package mysql

import (
	"context"
	"database/sql"
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
	Comment  string `json:"comment"`
}
type Query struct {
	Mode  string `json:"mode,omitempty"`
	SQL   string `json:"sql"`
	Limit int    `json:"limit"`
	Skip  int    `json:"skip"`
}
type Result struct {
	Rows    [][]any  `json:"rows"`
	Columns []Column `json:"columns"`
	HasMore bool     `json:"has_more"`
}
type Reader interface {
	RefreshTables(context.Context, string) ([]string, error)
	RefreshColumns(context.Context, string, string) ([]Column, error)
	Metadata() *Catalog
	Refresh(context.Context) (*Catalog, error)
	Databases(context.Context) ([]string, error)
	Tables(context.Context, string) ([]string, error)
	Schema(context.Context, string, string) ([]Column, error)
	Execute(context.Context, string, Query) (Result, error)
}
type Client struct {
	DB        *sql.DB
	catalogMu sync.RWMutex
	refreshMu sync.Mutex
	catalog   *Catalog
}
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
	// Multi-statements and local file loading remain disabled.
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
	if catalog := c.Metadata(); catalog != nil {
		return catalog.Databases, nil
	}
	return list(ctx, c.DB, "SHOW DATABASES")
}
func (c *Client) Tables(ctx context.Context, db string) ([]string, error) {
	if catalog := c.Metadata(); catalog != nil {
		names := []string{}
		for name := range catalog.Schemas[db] {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}
	return list(ctx, c.DB, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", db)
}
func (c *Client) columns(ctx context.Context, db, table string) ([]Column, error) {
	if catalog := c.Metadata(); catalog != nil {
		columns := catalog.Schemas[db][table]
		if columns == nil {
			columns = []Column{}
		}
		return columns, nil
	}
	return c.liveColumns(ctx, db, table)
}
func (c *Client) liveColumns(ctx context.Context, db, table string) ([]Column, error) {
	rows, err := c.DB.QueryContext(ctx, "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", db, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Column{}
	for rows.Next() {
		var col Column
		var nullable string
		if err := rows.Scan(&col.Name, &col.Type, &nullable, &col.Key, &col.Comment); err != nil {
			return nil, err
		}
		col.Nullable = nullable == "YES"
		out = append(out, col)
	}
	return out, rows.Err()
}

// quoteIdentifier handles MySQL identifiers independently from parameter values.
func quoteIdentifier(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

// Schema loads columns for editor completion without querying table data.
func (c *Client) Schema(ctx context.Context, database, table string) ([]Column, error) {
	return c.columns(ctx, database, table)
}
