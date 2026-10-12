package mysql

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// ParseSQL validates the AST, then reserializes it so executable comments cannot
// introduce operations that were invisible to the parser.
func ParseSQL(statement string) (sqlparser.SelectStatement, error) {
	node, err := sqlparser.Parse(statement)
	if err != nil {
		return nil, fmt.Errorf("SQL 语法不受支持，请输入单条 SELECT")
	}
	selectNode, ok := node.(sqlparser.SelectStatement)
	if !ok {
		return nil, fmt.Errorf("仅支持单条 SELECT 查询")
	}
	err = sqlparser.Walk(func(n sqlparser.SQLNode) (bool, error) {
		switch v := n.(type) {
		case *sqlparser.Select:
			if v.Lock != "" {
				return false, fmt.Errorf("不支持锁定查询")
			}
			v.Comments = nil
		case *sqlparser.Union:
			if v.Lock != "" {
				return false, fmt.Errorf("不支持锁定查询")
			}
		case *sqlparser.FuncExpr:
			// Unknown stored functions may write or have external effects. Only known
			// data transformation and aggregation functions are accepted.
			allowed := " count sum avg min max abs ceil ceiling floor round truncate mod concat concat_ws length char_length lower upper trim ltrim rtrim substring substr left right replace coalesce ifnull nullif if date year month day dayofmonth hour minute second date_format datediff timestampdiff json_extract json_unquote json_length now current_timestamp curdate cast convert greatest least group_concat find_in_set hex unhex "
			if !v.Qualifier.IsEmpty() || !strings.Contains(allowed, " "+strings.ToLower(v.Name.String())+" ") {
				return false, fmt.Errorf("查询包含不支持的函数")
			}
		}
		return true, nil
	}, selectNode)
	return selectNode, err
}

func pageSQL(node sqlparser.SelectStatement, q Query) (string, error) {
	if q.Limit < 1 || q.Limit > 200 || q.Skip < 0 || q.Skip > 1000000 {
		return "", fmt.Errorf("invalid pagination")
	}
	var old *sqlparser.Limit
	switch n := node.(type) {
	case *sqlparser.Select:
		old = n.Limit
	case *sqlparser.Union:
		old = n.Limit
	default:
		return "", fmt.Errorf("unsupported SELECT")
	}
	offset, count := int64(q.Skip), int64(q.Limit+1)
	if old != nil {
		number := func(e sqlparser.Expr) (int64, error) {
			v, ok := e.(*sqlparser.SQLVal)
			if !ok || v.Type != sqlparser.IntVal {
				return 0, fmt.Errorf("LIMIT 必须使用非负整数")
			}
			n, err := strconv.ParseInt(string(v.Val), 10, 64)
			if err != nil || n < 0 {
				return 0, fmt.Errorf("LIMIT 超出范围")
			}
			return n, nil
		}
		total, err := number(old.Rowcount)
		if err != nil {
			return "", err
		}
		remaining := total - int64(q.Skip)
		if remaining < 0 {
			remaining = 0
		}
		if remaining < count {
			count = remaining
		}
		if old.Offset != nil {
			start, err := number(old.Offset)
			if err != nil {
				return "", err
			}
			if start > 1000000 {
				return "", fmt.Errorf("OFFSET 超出范围")
			}
			offset += start
		}
	}
	node.SetLimit(&sqlparser.Limit{Offset: sqlparser.NewIntVal([]byte(strconv.FormatInt(offset, 10))), Rowcount: sqlparser.NewIntVal([]byte(strconv.FormatInt(count, 10)))})
	return sqlparser.String(node), nil
}

type source struct {
	database, table, alias string
	columns                []Column
}

func (c *Client) resultColumns(ctx context.Context, database string, node sqlparser.SelectStatement, names []string) []Column {
	result := make([]Column, len(names))
	for i, n := range names {
		result[i] = Column{Name: n}
	}
	selectNode, ok := node.(*sqlparser.Select)
	if !ok {
		return result
	}
	sources := []source{}
	var collect func(sqlparser.TableExpr)
	collect = func(expr sqlparser.TableExpr) {
		switch e := expr.(type) {
		case *sqlparser.AliasedTableExpr:
			if t, ok := e.Expr.(sqlparser.TableName); ok {
				db := t.Qualifier.String()
				if db == "" {
					db = database
				}
				alias := e.As.String()
				if alias == "" {
					alias = t.Name.String()
				}
				cols, err := c.columns(ctx, db, t.Name.String())
				if err == nil {
					sources = append(sources, source{db, t.Name.String(), alias, cols})
				}
			}
		case *sqlparser.JoinTableExpr:
			collect(e.LeftExpr)
			collect(e.RightExpr)
		case *sqlparser.ParenTableExpr:
			for _, t := range e.Exprs {
				collect(t)
			}
		}
	}
	for _, t := range selectNode.From {
		collect(t)
	}
	projected := []Column{}
	for _, expr := range selectNode.SelectExprs {
		switch e := expr.(type) {
		case *sqlparser.StarExpr:
			matched := false
			for _, s := range sources {
				if e.TableName.IsEmpty() || e.TableName.Name.String() == s.alias || e.TableName.Name.String() == s.table {
					projected = append(projected, s.columns...)
					matched = true
				}
			}
			if !matched {
				return result
			}
		case *sqlparser.AliasedExpr:
			col := Column{}
			if field, ok := e.Expr.(*sqlparser.ColName); ok {
				candidates := []Column{}
				for _, s := range sources {
					qual := field.Qualifier
					if !qual.IsEmpty() && qual.Name.String() != s.alias && qual.Name.String() != s.table {
						continue
					}
					if !qual.Qualifier.IsEmpty() && qual.Qualifier.String() != s.database {
						continue
					}
					for _, c := range s.columns {
						if strings.EqualFold(c.Name, field.Name.String()) {
							candidates = append(candidates, c)
						}
					}
				}
				if len(candidates) == 1 {
					col = candidates[0]
				}
			}
			projected = append(projected, col)
		default:
			return result
		}
	}
	// JOIN USING, views or derived tables may expand differently. Never attach a
	// comment to the wrong result column when projection width is uncertain.
	if len(projected) != len(names) {
		return result
	}
	for i, col := range projected {
		col.Name = names[i]
		result[i] = col
	}
	return result
}
func (c *Client) Execute(ctx context.Context, database string, q Query) (Result, error) {
	out := Result{Rows: [][]any{}, Columns: []Column{}}
	node, err := ParseSQL(q.SQL)
	if err != nil {
		return out, err
	}
	// A pooled connection may retain a previous USE database. Require explicit
	// qualification when no database is selected to prevent querying that stale database.
	if database == "" {
		err := sqlparser.Walk(func(n sqlparser.SQLNode) (bool, error) {
			if table, ok := n.(*sqlparser.AliasedTableExpr); ok {
				if name, ok := table.Expr.(sqlparser.TableName); ok && name.Qualifier.IsEmpty() && name.Name.String() != "dual" {
					return false, fmt.Errorf("请选择数据库，或在 SQL 中使用完整表名")
				}
			}
			return true, nil
		}, node)
		if err != nil {
			return out, err
		}
	}
	statement, err := pageSQL(node, q)
	if err != nil {
		return out, err
	}
	tx, err := c.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if database != "" {
		if _, err := tx.ExecContext(ctx, "USE "+quoteIdentifier(database)); err != nil {
			return out, err
		}
	}
	rows, err := tx.QueryContext(ctx, statement)
	if err != nil {
		return out, err
	}
	names, err := rows.Columns()
	if err != nil {
		rows.Close()
		return out, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		return out, err
	}
	size := 0
	for rows.Next() {
		if len(out.Rows) == q.Limit {
			out.HasMore = true
			break
		}
		values := make([]any, len(names))
		dest := make([]any, len(names))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return out, err
		}
		for i, value := range values {
			switch v := value.(type) {
			case []byte:
				typ := types[i].DatabaseTypeName()
				if strings.Contains(typ, "BLOB") || typ == "BINARY" || typ == "VARBINARY" || typ == "BIT" || typ == "GEOMETRY" {
					values[i] = map[string]string{"base64": base64.StdEncoding.EncodeToString(v)}
				} else {
					values[i] = string(v)
				}
			case int64, uint64:
				values[i] = fmt.Sprint(v)
			}
		}
		raw, err := json.Marshal(values)
		if err != nil {
			rows.Close()
			return out, err
		}
		size += len(raw)
		if size > 32*1024*1024 {
			rows.Close()
			return out, fmt.Errorf("result too large")
		}
		out.Rows = append(out.Rows, values)
	}
	err = rows.Err()
	rows.Close()
	tx.Rollback()
	if err != nil {
		return out, err
	}
	out.Columns = c.resultColumns(ctx, database, node, names)
	for i := range out.Columns {
		if out.Columns[i].Type == "" {
			out.Columns[i].Type = types[i].DatabaseTypeName()
		}
	}
	return out, nil
}
