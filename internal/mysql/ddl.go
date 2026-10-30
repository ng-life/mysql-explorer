package mysql

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// ddlStatement keeps the original definition: the legacy AST intentionally
// omits ALTER clauses and index definitions. Never execute its serialized DDL.
func ddlStatement(statement string) (string, bool, error) {
	lexer := sqlparser.NewStringTokenizer(statement)
	first, _ := lexer.Scan()
	switch first {
	case sqlparser.CREATE, sqlparser.ALTER, sqlparser.DROP, sqlparser.TRUNCATE, sqlparser.RENAME:
	default:
		return "", false, nil
	}
	invalid := func() (string, bool, error) {
		return "", true, fmt.Errorf("DDL 仅支持单条表、索引或数据库定义；不支持注释、CREATE SELECT、视图及存储程序")
	}
	// Recognize quotes before checking comment markers. Reject executable comments
	// rather than risk treating hidden SQL as ordinary schema text.
	for i := 0; i < len(statement); i++ {
		ch := statement[i]
		if ch == '\'' || ch == '"' || ch == '`' {
			quote := ch
			closed := false
			for i++; i < len(statement); i++ {
				if statement[i] == '\\' {
					i++
					continue
				}
				if statement[i] == quote {
					if i+1 < len(statement) && statement[i+1] == quote {
						i++
						continue
					}
					closed = true
					break
				}
			}
			if !closed {
				return invalid()
			}
		} else if ch == '#' || (i+1 < len(statement) && ((ch == '/' && (statement[i+1] == '*' || statement[i+1] == '/')) || (ch == '-' && statement[i+1] == '-'))) {
			return invalid()
		}
	}
	lexer = sqlparser.NewStringTokenizer(statement)
	tokens := []int{}
	ended := false
	for {
		kind, _ := lexer.Scan()
		if kind == 0 {
			break
		}
		if kind == sqlparser.LEX_ERROR {
			return invalid()
		}
		if ended {
			return invalid()
		}
		if kind == ';' {
			ended = true
			continue
		}
		if kind == sqlparser.SELECT {
			return invalid()
		}
		tokens = append(tokens, kind)
	}
	if len(tokens) < 2 {
		return invalid()
	}
	object := tokens[1]
	if first == sqlparser.CREATE && (object == sqlparser.UNIQUE || object == sqlparser.FULLTEXT || object == sqlparser.SPATIAL) {
		if len(tokens) < 3 {
			return invalid()
		}
		object = tokens[2]
	}
	switch first {
	case sqlparser.CREATE, sqlparser.DROP:
		if object != sqlparser.TABLE && object != sqlparser.INDEX && object != sqlparser.DATABASE && object != sqlparser.SCHEMA {
			return invalid()
		}
	case sqlparser.ALTER:
		if object != sqlparser.TABLE {
			return invalid()
		}
	case sqlparser.RENAME:
		if object != sqlparser.TABLE {
			return invalid()
		}
	}
	clean := strings.TrimSpace(statement)
	clean = strings.TrimSpace(strings.TrimSuffix(clean, ";"))
	return clean, true, nil
}

func isDDL(node sqlparser.Statement) bool {
	switch node.(type) {
	case *sqlparser.DDL, *sqlparser.DBDDL:
		return true
	default:
		return false
	}
}

func (c *Client) executeDDL(ctx context.Context, database, statement string, node sqlparser.Statement) (Result, error) {
	out := Result{Rows: [][]any{}, Columns: []Column{}}
	if _, databaseDDL := node.(*sqlparser.DBDDL); !databaseDDL && database == "" {
		return out, fmt.Errorf("表或索引 DDL 必须选择数据库，CLI 使用 --schema")
	}
	// MySQL/TiDB DDL may implicitly commit. Use one dedicated pooled connection,
	// never promise rollback by wrapping schema changes in a transaction.
	conn, err := c.DB.Conn(ctx)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	if database != "" {
		if _, err = conn.ExecContext(ctx, "USE "+quoteIdentifier(database)); err != nil {
			return out, err
		}
	}
	result, err := conn.ExecContext(ctx, statement)
	if err != nil {
		return out, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return out, err
	}
	value := strconv.FormatInt(affected, 10)
	out.AffectedRows = &value
	out.StatementType = "ddl"
	return out, nil
}
