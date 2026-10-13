package mysql

import (
	"context"
	"database/sql"
	"fmt"
)

type TableInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Comment string `json:"comment"`
}

// Field keeps SQL NULL distinct from an empty default value.
type Field struct {
	Position int     `json:"position"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
	Key      string  `json:"key"`
	Extra    string  `json:"extra"`
	Comment  string  `json:"comment"`
}

type Index struct {
	Name     string  `json:"name"`
	Unique   bool    `json:"unique"`
	Position int     `json:"position"`
	Column   *string `json:"column"`
	Prefix   *int64  `json:"prefix"`
	Type     string  `json:"type"`
}

func (c *Client) TableInfos(ctx context.Context, schema string) ([]TableInfo, error) {
	rows, err := c.DB.QueryContext(ctx, "SELECT TABLE_NAME, TABLE_TYPE, TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TableInfo{}
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Type, &t.Comment); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (c *Client) Fields(ctx context.Context, schema, table string) ([]Field, error) {
	rows, err := c.DB.QueryContext(ctx, "SELECT ORDINAL_POSITION, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, COLUMN_KEY, EXTRA, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Field{}
	for rows.Next() {
		var f Field
		var nullable string
		var def sql.NullString
		if err := rows.Scan(&f.Position, &f.Name, &f.Type, &nullable, &def, &f.Key, &f.Extra, &f.Comment); err != nil {
			return nil, err
		}
		f.Nullable = nullable == "YES"
		if def.Valid {
			f.Default = &def.String
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("表不存在或无权访问")
	}
	return out, nil
}

func (c *Client) Indexes(ctx context.Context, schema, table string) ([]Index, error) {
	rows, err := c.DB.QueryContext(ctx, "SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME, SUB_PART, INDEX_TYPE FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX", schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Index{}
	for rows.Next() {
		var idx Index
		var nonUnique int
		var col sql.NullString
		var prefix sql.NullInt64
		if err := rows.Scan(&idx.Name, &nonUnique, &idx.Position, &col, &prefix, &idx.Type); err != nil {
			return nil, err
		}
		idx.Unique = nonUnique == 0
		if col.Valid {
			idx.Column = &col.String
		}
		if prefix.Valid {
			idx.Prefix = &prefix.Int64
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

func (c *Client) DDL(ctx context.Context, schema, table string) (string, error) {
	// Identifiers cannot use value placeholders; quote each component independently.
	rows, err := c.DB.QueryContext(ctx, "SHOW CREATE TABLE "+quoteIdentifier(schema)+"."+quoteIdentifier(table))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("表不存在或无权访问")
	}
	values := make([]sql.NullString, len(names))
	dest := make([]any, len(names))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return "", err
	}
	if len(values) < 2 {
		return "", fmt.Errorf("建表定义不可用")
	}
	return values[1].String, rows.Err()
}
