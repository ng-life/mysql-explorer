package mysql

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"testing"
)

func TestDDLPolicy(t *testing.T) {
	for _, statement := range []string{
		"CREATE TABLE sales.t(id bigint PRIMARY KEY)",
		"ALTER TABLE sales.t ADD COLUMN name varchar(100) DEFAULT 'a;b'",
		"CREATE INDEX ix_id ON sales.t(id)",
		"DROP INDEX ix_id ON sales.t",
		"DROP TABLE sales.t", "TRUNCATE TABLE sales.t",
		"RENAME TABLE sales.t TO sales.t2", "CREATE DATABASE example", "DROP DATABASE example",
	} {
		for _, dml := range []bool{false, true} {
			if err := ValidateQuery(Query{SQL: statement}, dml, false); err == nil {
				t.Fatal("DDL enabled by DML flag", statement)
			}
		}
		if err := ValidateQuery(Query{SQL: statement}, false, true); err != nil {
			t.Fatal(statement, err)
		}
		if err := ValidateQuery(Query{SQL: statement, Skip: 1}, true, true); err == nil {
			t.Fatal("DDL pagination accepted")
		}
		if err := ValidateQuery(Query{SQL: statement, Mode: "explain_analyze"}, true, true); err == nil {
			t.Fatal("DDL explain accepted")
		}
	}
	for _, statement := range []string{
		"ALTER TABLE sales.t ADD id int; DROP TABLE sales.t",
		"CREATE TABLE sales.t(id int); SELECT 1",
		"CREATE TABLE sales.t AS SELECT 1",
		"ALTER TABLE sales.t ADD id int /*!; DROP TABLE sales.t */",
		"/* ordinary */ DROP TABLE sales.t", "CREATE VIEW sales.v AS SELECT 1",
		"CREATE PROCEDURE sales.p() BEGIN DELETE FROM sales.t; END",
		"CREATE TRIGGER tr AFTER INSERT ON sales.t FOR EACH ROW DELETE FROM sales.t2",
		"GRANT ALL ON *.* TO user", "SET autocommit=0", "CREATE TEMPORARY TABLE sales.t(id int)",
	} {
		if err := ValidateQuery(Query{SQL: statement}, true, true); err == nil {
			t.Fatal("unsafe DDL accepted", statement)
		}
	}
	if err := ValidateQuery(Query{SQL: "DELETE FROM sales.t"}, false, true); err == nil {
		t.Fatal("DDL flag enabled DML")
	}
}

func TestDDLExecutesFullDefinitionWithoutTransaction(t *testing.T) {
	for _, engine := range []string{"mysql", "tidb"} {
		for _, statement := range []string{"ALTER TABLE sales.t ADD COLUMN name varchar(100) DEFAULT 'a;b'", "CREATE INDEX ix_name ON sales.t(name)", "DROP INDEX ix_name ON sales.t"} {
			for _, failure := range []bool{false, true} {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				mock.ExpectExec("USE `sales`").WillReturnResult(sqlmock.NewResult(0, 0))
				expected := mock.ExpectExec("^" + regexp.QuoteMeta(statement) + "$")
				if failure {
					expected.WillReturnError(errors.New("DDL failed"))
				} else {
					expected.WillReturnResult(sqlmock.NewResult(0, 0))
				}
				out, err := (&Client{DB: db, Type: engine, AllowDDL: true}).Execute(context.Background(), "sales", Query{SQL: statement + ";"})
				if failure {
					if err == nil {
						t.Fatal("error hidden")
					}
				} else if err != nil || out.StatementType != "ddl" || out.AffectedRows == nil || *out.AffectedRows != "0" {
					t.Fatal(out, err)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
				db.Close()
			}
		}
	}
	if _, err := (&Client{AllowDDL: true}).Execute(context.Background(), "", Query{SQL: "ALTER TABLE sales.t ADD x int"}); err == nil {
		t.Fatal("database context required")
	}
}

func TestDatabaseDDLWithoutSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("^CREATE DATABASE example$").WillReturnResult(sqlmock.NewResult(0, 0))
	out, err := (&Client{DB: db, AllowDDL: true}).Execute(context.Background(), "", Query{SQL: "CREATE DATABASE example"})
	if err != nil || out.StatementType != "ddl" {
		t.Fatal(out, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
