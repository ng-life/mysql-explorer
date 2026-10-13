package mysql

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"testing"
)

func TestStructureDefaultsAndBoundNames(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := &Client{DB: db}
	mock.ExpectQuery("SELECT ORDINAL_POSITION").WithArgs("s'", "t`").WillReturnRows(sqlmock.NewRows([]string{"pos", "name", "type", "nullable", "default", "key", "extra", "comment"}).AddRow(1, "a", "varchar(10)", "YES", nil, "", "", "note").AddRow(2, "b", "varchar(10)", "NO", "", "", "", ""))
	fields, err := c.Fields(context.Background(), "s'", "t`")
	if err != nil || len(fields) != 2 || fields[0].Default != nil || fields[1].Default == nil || *fields[1].Default != "" {
		t.Fatal(fields, err)
	}
	mock.ExpectQuery("SELECT INDEX_NAME").WithArgs("s'", "t`").WillReturnRows(sqlmock.NewRows([]string{"name", "non_unique", "pos", "col", "prefix", "type"}).AddRow("PRIMARY", 0, 1, "a", nil, "BTREE").AddRow("expression", 1, 1, nil, 3, "BTREE"))
	indexes, err := c.Indexes(context.Background(), "s'", "t`")
	if err != nil || !indexes[0].Unique || indexes[1].Column != nil || *indexes[1].Prefix != 3 {
		t.Fatal(indexes, err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SHOW CREATE TABLE `s'`.`t```")).WillReturnRows(sqlmock.NewRows([]string{"Table", "Create Table"}).AddRow("t`", "CREATE TABLE ..."))
	if ddl, err := c.DDL(context.Background(), "s'", "t`"); err != nil || ddl != "CREATE TABLE ..." {
		t.Fatal(ddl, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMissingTable(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	c := &Client{DB: db}
	mock.ExpectQuery("SELECT ORDINAL_POSITION").WithArgs("s", "missing").WillReturnRows(sqlmock.NewRows([]string{"pos", "name", "type", "nullable", "default", "key", "extra", "comment"}))
	if _, err := c.Fields(context.Background(), "s", "missing"); err == nil {
		t.Fatal("missing table accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
