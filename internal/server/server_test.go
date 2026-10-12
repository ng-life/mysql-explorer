package server

import (
	"context"
	"encoding/json"
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
func (fake) Find(context.Context, string, string, mysql.Query) (mysql.Result, error) {
	return mysql.Result{Documents: []map[string]any{{"id": "9007199254740993"}}, Columns: []mysql.Column{{Name: "id", Key: "PRI"}}}, nil
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
	for _, body := range []string{`{"database":"shop","table":"orders","filter":{"id":{"$gt":1}}}`, `{"database":"shop","table":"orders","sort":{"id":2}}`, `{"database":"shop","table":"orders","sql":"DELETE FROM orders"}`, `{"database":"shop","table":"orders"} {}`, `{"database":"shop","table":"orders","filter":null}`} {
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
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/clusters/0/rows/query", strings.NewReader(`{"database":"shop","table":"orders"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}
func TestParsePreservesNumber(t *testing.T) {
	q, err := parseQuery(queryRequest{Filter: json.RawMessage(`{"id":9007199254740993}`)})
	if err != nil || q.Filter["id"] != json.Number("9007199254740993") {
		t.Fatalf("%v %v", q, err)
	}
}
