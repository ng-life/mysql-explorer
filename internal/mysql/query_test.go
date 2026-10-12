package mysql

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const metadataSQL = "SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION"

func metadata(mock sqlmock.Sqlmock, db, table string, columns ...[]string) {
	rows := sqlmock.NewRows([]string{"name", "type", "nullable", "key", "comment"})
	for _, c := range columns {
		rows.AddRow(c[0], c[1], c[2], c[3], c[4])
	}
	mock.ExpectQuery(regexp.QuoteMeta(metadataSQL)).WithArgs(db, table).WillReturnRows(rows)
}
func TestCommentsResolveSQLSources(t *testing.T) {
	cases := []struct {
		sql             string
		names, comments []string
	}{
		{"SELECT * FROM sales.orders", []string{"id", "customer_id"}, []string{"订单编号", "客户编号"}},
		{"SELECT o.id AS order_no, c.id AS customer_no, count(*) AS total FROM sales.orders o JOIN crm.customers c ON o.customer_id=c.id GROUP BY o.id,c.id", []string{"order_no", "customer_no", "total"}, []string{"订单编号", "客户主键", ""}},
		{"SELECT o.*, c.id FROM sales.orders o JOIN crm.customers c ON o.customer_id=c.id", []string{"id", "customer_id", "id"}, []string{"订单编号", "客户编号", "客户主键"}},
	}
	for _, tc := range cases {
		t.Run(tc.sql, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			metadata(mock, "sales", "orders", []string{"id", "bigint", "NO", "PRI", "订单编号"}, []string{"customer_id", "bigint", "YES", "", "客户编号"})
			if len(tc.comments) == 3 {
				metadata(mock, "crm", "customers", []string{"id", "bigint", "NO", "PRI", "客户主键"})
			}
			node, err := ParseSQL(tc.sql)
			if err != nil {
				t.Fatal(err)
			}
			cols := (&Client{DB: db}).resultColumns(context.Background(), "unused", node, tc.names)
			for i, col := range cols {
				if col.Name != tc.names[i] || col.Comment != tc.comments[i] {
					t.Fatalf("column %d: %+v", i, col)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestExecutePreservesDuplicateColumnsAndPrecision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("USE `sales`").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("select id, id from orders limit 0, 21")).WillReturnRows(sqlmock.NewRowsWithColumnDefinition(sqlmock.NewColumn("id").OfType("BIGINT", int64(0)), sqlmock.NewColumn("id").OfType("BIGINT", int64(0))).AddRow(int64(9007199254740993), int64(9007199254740993)))
	mock.ExpectRollback()
	metadata(mock, "sales", "orders", []string{"id", "bigint", "NO", "PRI", "编号"})
	out, err := (&Client{DB: db}).Execute(context.Background(), "sales", Query{SQL: "SELECT id,id FROM orders", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Columns) != 2 || len(out.Rows) != 1 || len(out.Rows[0]) != 2 || out.Rows[0][1] != "9007199254740993" {
		t.Fatalf("wrong result: %+v", out)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNoImplicitDatabaseFromPreviousConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = (&Client{DB: db}).Execute(context.Background(), "", Query{SQL: "SELECT * FROM orders", Limit: 20})
	if err == nil {
		t.Fatal("accepted unqualified table without selected database")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
