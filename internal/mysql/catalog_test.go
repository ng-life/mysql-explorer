package mysql

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ng-life/mysql-explorer/internal/config"
)

const allTablesSQL = "SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.TABLES ORDER BY TABLE_SCHEMA, TABLE_NAME"
const allColumnsSQL = "SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_COMMENT FROM information_schema.COLUMNS ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION"

func expectCatalog(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SHOW DATABASES").WillReturnRows(sqlmock.NewRows([]string{"database"}).AddRow("shop").AddRow("empty"))
	mock.ExpectQuery(regexp.QuoteMeta(allTablesSQL)).WillReturnRows(sqlmock.NewRows([]string{"db", "table"}).AddRow("shop", "orders").AddRow("hidden", "secret"))
	mock.ExpectQuery(regexp.QuoteMeta(allColumnsSQL)).WillReturnRows(sqlmock.NewRows([]string{"db", "table", "name", "type", "nullable", "key", "comment"}).AddRow("shop", "orders", "id", "bigint", "NO", "PRI", "编号").AddRow("shop", "orders", "amount", "decimal(10,2)", "YES", "", "金额").AddRow("hidden", "secret", "token", "text", "YES", "", "secret"))
}
func TestPreloadAndTargetedRefresh(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := &Client{DB: db}
	expectCatalog(mock)
	original, err := c.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Databases) != 2 || len(original.Schemas["shop"]["orders"]) != 2 || original.Schemas["hidden"] != nil {
		t.Fatalf("invalid catalog: %+v", original)
	}
	if _, err := c.Tables(context.Background(), "shop"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Schema(context.Background(), "shop", "orders"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME")).WithArgs("shop").WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("new_orders"))
	if _, err := c.RefreshTables(context.Background(), "shop"); err != nil {
		t.Fatal(err)
	}
	if _, exists := c.Metadata().Schemas["shop"]["orders"]; exists {
		t.Fatal("dropped table retained")
	}
	if len(original.Schemas["shop"]["orders"]) != 2 {
		t.Fatal("old snapshot mutated")
	}
	metadata(mock, "shop", "new_orders", []string{"new_id", "bigint", "NO", "PRI", "新编号"})
	if _, err := c.RefreshColumns(context.Background(), "shop", "new_orders"); err != nil {
		t.Fatal(err)
	}
	if c.Metadata().Schemas["shop"]["new_orders"][0].Comment != "新编号" {
		t.Fatal("new columns not cached")
	}
	previous := c.Metadata()
	mock.ExpectQuery("SHOW DATABASES").WillReturnError(errors.New("offline"))
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("failed refresh accepted")
	}
	if c.Metadata() != previous {
		t.Fatal("failed refresh replaced snapshot")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestStartupServiceFailureIsolation(t *testing.T) {
	good, m1, _ := sqlmock.New()
	defer good.Close()
	bad, m2, _ := sqlmock.New()
	defer bad.Close()
	expectCatalog(m1)
	m2.ExpectQuery("SHOW DATABASES").WillReturnError(errors.New("offline"))
	pool := NewPool([]config.Cluster{{Name: "good"}, {Name: "bad"}})
	pool.clients[0] = &Client{DB: good}
	pool.clients[1] = &Client{DB: bad}
	pool.Preload(context.Background())
	if pool.clients[0].Metadata() == nil || pool.clients[1].Metadata() != nil {
		t.Fatal("service metadata mixed or failure not isolated")
	}
	if err := m1.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := m2.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
