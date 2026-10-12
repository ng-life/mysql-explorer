package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ng-life/mysql-explorer/internal/config"
	"github.com/ng-life/mysql-explorer/internal/mysql"
)

type fake struct{}

func (fake) Get(context.Context, int) (mysql.Reader, error)   { return fake{}, nil }
func (fake) Databases(context.Context) ([]string, error)      { return []string{"shop"}, nil }
func (fake) Tables(context.Context, string) ([]string, error) { return []string{"orders"}, nil }
func (fake) Execute(context.Context, string, mysql.Query) (mysql.Result, error) {
	return mysql.Result{Rows: [][]any{{"9007199254740993"}}, Columns: []mysql.Column{{Name: "id", Key: "PRI"}}}, nil
}
func TestReadOnlyHTTP(t *testing.T) {
	h := New(config.Config{Clusters: []config.Cluster{{Name: "test", Address: "localhost:3306", User: "private-user", Password: "private-password"}}}, fake{}).Handler()
	for _, path := range []string{"/", "/api/clusters", "/api/clusters/0/databases", "/api/clusters/0/tables?db=shop"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-password") || strings.Contains(w.Body.String(), "private-user") {
			t.Fatalf("unsafe response %d", w.Code)
		}
	}
	for _, body := range []string{`{"database":"shop","sql":"DELETE FROM orders"}`, `{"database":"shop","sql":"SELECT 1; SELECT 2"}`, `{"database":"shop","sql":"SELECT 1","limit":201}`, `{"database":"shop","sql":"SELECT 1"} {}`} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/clusters/0/rows/query", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("accepted invalid request: %s", body)
		}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/clusters/0/rows/query", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/clusters/0/rows/query", strings.NewReader(`{"database":"shop","sql":"SELECT * FROM shop.orders"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
