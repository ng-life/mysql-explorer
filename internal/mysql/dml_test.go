package mysql

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/xwb1989/sqlparser"
)

func TestDMLPolicy(t *testing.T) {
	for _, statement := range []string{
		"INSERT INTO sales.orders(id) VALUES(1)",
		"REPLACE INTO sales.orders(id) VALUES(1)",
		"UPDATE sales.orders SET id=2 WHERE id=1",
		"DELETE FROM sales.orders WHERE id=1",
		"INSERT INTO sales.orders(id) SELECT id FROM sales.archive",
	} {
		if err := ValidateQuery(Query{SQL: statement}, false, false); err == nil {
			t.Fatal("DML accepted by default", statement)
		}
		if err := ValidateQuery(Query{SQL: statement}, true, false); err != nil {
			t.Fatal(statement, err)
		}
		for _, mode := range []string{"explain", "explain_analyze"} {
			if err := ValidateQuery(Query{SQL: statement, Mode: mode}, true, false); err == nil {
				t.Fatal("DML plan accepted", mode, statement)
			}
		}
		if err := ValidateQuery(Query{SQL: statement, Skip: 1}, true, false); err == nil {
			t.Fatal("DML pagination accepted")
		}
	}
	for _, statement := range []string{
		"DROP TABLE sales.orders", "TRUNCATE TABLE sales.orders", "CREATE TABLE sales.test(id int)",
		"SET tidb_enable_noop_functions=ON", "CALL dangerous()", "BEGIN",
		"UPDATE sales.orders SET id=custom_function()", "INSERT INTO sales.orders(id) VALUES(sleep(10))",
		"DELETE FROM sales.orders; SELECT 1", "INSERT INTO sales.orders SELECT * FROM sales.archive FOR UPDATE",
	} {
		if err := ValidateQuery(Query{SQL: statement}, true, false); err == nil {
			t.Fatal("unsafe statement accepted", statement)
		}
	}
	for _, statement := range []string{
		"INSERT /* ordinary */ INTO sales.orders(id) VALUES(1)",
		"UPDATE /* ordinary */ sales.orders SET id=1",
		"DELETE /* ordinary */ FROM sales.orders",
	} {
		node, err := parseQuery(Query{SQL: statement}, true, false)
		if err != nil || strings.Contains(sqlparser.String(node), "ordinary") {
			t.Fatal("comment retained", err)
		}
	}
}

func TestDMLCommitAndRollback(t *testing.T) {
	for _, engine := range []string{"mysql", "tidb"} {
		for _, statement := range []string{"insert into sales.orders(id) values (1)", "replace into sales.orders(id) values (1)", "update sales.orders set id = 2 where id = 1", "delete from sales.orders where id = 1"} {
			for _, failure := range []string{"", "exec", "commit"} {
				t.Run(engine+"/"+statement+"/"+failure, func(t *testing.T) {
					db, mock, err := sqlmock.New()
					if err != nil {
						t.Fatal(err)
					}
					defer db.Close()
					mock.ExpectBegin()
					execution := mock.ExpectExec("^" + regexp.QuoteMeta(statement) + "$")
					switch failure {
					case "exec":
						execution.WillReturnError(errors.New("execution failed"))
						mock.ExpectRollback()
					case "commit":
						execution.WillReturnResult(sqlmock.NewResult(9007199254740993, 2))
						mock.ExpectCommit().WillReturnError(errors.New("connection lost during commit"))
					default:
						execution.WillReturnResult(sqlmock.NewResult(9007199254740993, 2))
						mock.ExpectCommit()
					}
					out, err := (&Client{DB: db, Type: engine, AllowDML: true}).Execute(context.Background(), "", Query{SQL: statement, Limit: 20})
					if failure == "" {
						if err != nil || out.AffectedRows == nil || *out.AffectedRows != "2" || out.HasMore {
							t.Fatal(out, err)
						}
						insert := strings.HasPrefix(statement, "insert") || strings.HasPrefix(statement, "replace")
						if insert && (out.LastInsertID == nil || *out.LastInsertID != "9007199254740993") {
							t.Fatal(out)
						}
						if !insert && out.LastInsertID != nil {
							t.Fatal(out)
						}
					} else if err == nil {
						t.Fatal("failure hidden")
					}
					if err := mock.ExpectationsWereMet(); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}

func TestDMLRequiresDatabase(t *testing.T) {
	for _, statement := range []string{"INSERT INTO orders(id) VALUES(1)", "REPLACE INTO orders(id) VALUES(1)", "UPDATE orders SET id=1", "DELETE FROM orders", "INSERT INTO sales.orders SELECT * FROM archive"} {
		if _, err := (&Client{AllowDML: true}).Execute(context.Background(), "", Query{SQL: statement}); err == nil {
			t.Fatal(statement)
		}
	}
}

func TestTiDBQueryAndPlanResults(t *testing.T) {
	for _, mode := range []string{"query", "explain", "explain_analyze"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			statement := "select 1 from dual limit 0, 21"
			if mode == "explain" {
				statement = "EXPLAIN select 1 from dual"
			}
			if mode == "explain_analyze" {
				statement = "EXPLAIN ANALYZE select 1 from dual"
			}
			mock.ExpectBegin()
			mock.ExpectQuery("^" + regexp.QuoteMeta(statement) + "$").WillReturnRows(sqlmock.NewRows([]string{"id", "actRows"}).AddRow("Projection_1", "1"))
			mock.ExpectRollback()
			out, err := (&Client{DB: db, Type: "tidb"}).Execute(context.Background(), "", Query{SQL: "SELECT 1", Mode: mode, Limit: 20})
			if err != nil || len(out.Columns) != 2 || len(out.Rows) != 1 {
				t.Fatal(out, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
