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
	h := New(config.Config{Clusters: []config.Cluster{{Name: "test", Description: "web-hidden-description", Address: "localhost:3306", User: "private-user", Password: "private-password"}}}, fake{}).Handler()
	for _, path := range []string{"/", "/api/clusters", "/api/clusters/0/databases", "/api/clusters/0/tables?db=shop", "/api/clusters/0/columns?db=shop&table=orders", "/assets/editor.js", "/assets/result-grid.js", "/assets/result-grid.css", "/assets/result-grid.LICENSE.txt", "/api/clusters/0/metadata"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-password") || strings.Contains(w.Body.String(), "private-user") || strings.Contains(w.Body.String(), "web-hidden-description") {
			t.Fatalf("unsafe response %d", w.Code)
		}
	}
	for _, body := range []string{
		`{"sql":"SELECT 1","mode":"invalid"}`,
		`{"sql":"SELECT 1","mode":"explain","skip":20}`,
		`{"sql":"DELETE FROM orders","mode":"explain_analyze"}`, `{"database":"shop","sql":"DELETE FROM orders"}`, `{"database":"shop","sql":"SELECT 1; SELECT 2"}`, `{"database":"shop","sql":"SELECT 1","limit":201}`, `{"database":"shop","sql":"SELECT 1"} {}`} {
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

func (fake) Schema(context.Context, string, string) ([]mysql.Column, error) {
	return []mysql.Column{{Name: "id", Comment: "编号"}}, nil
}

func (fake) Metadata() *mysql.Catalog {
	return &mysql.Catalog{Databases: []string{"shop"}, Schemas: map[string]map[string][]mysql.Column{"shop": {"orders": {{Name: "id"}}}}}
}
func (f fake) Refresh(context.Context) (*mysql.Catalog, error) { return f.Metadata(), nil }

func (f fake) RefreshTables(ctx context.Context, db string) ([]string, error) {
	return f.Tables(ctx, db)
}
func (f fake) RefreshColumns(ctx context.Context, db, table string) ([]mysql.Column, error) {
	return f.Schema(ctx, db, table)
}

func TestExplainHTTPModes(t *testing.T) {
	h := New(config.Config{Clusters: []config.Cluster{{Name: "test"}}}, fake{}).Handler()
	for _, mode := range []string{"explain", "explain_analyze"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/clusters/0/rows/query", strings.NewReader(`{"sql":"SELECT 1","mode":"`+mode+`"}`)))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", mode, w.Code, w.Body.String())
		}
	}
}

type queryRecorder struct {
	fake
	calls int
	query mysql.Query
}

func (r *queryRecorder) Get(context.Context, int) (mysql.Reader, error) { r.calls++; return r, nil }
func (r *queryRecorder) Execute(_ context.Context, _ string, q mysql.Query) (mysql.Result, error) {
	r.query = q
	n := "0"
	return mysql.Result{AffectedRows: &n, Rows: [][]any{}, Columns: []mysql.Column{}}, nil
}
func TestDMLHTTPConnectionPolicy(t *testing.T) {
	reader := &queryRecorder{}
	h := New(config.Config{Clusters: []config.Cluster{{Name: "readonly", Type: "tidb"}, {Name: "writable", AllowDML: true}}}, reader).Handler()
	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"/api/clusters/0/rows/query", `{"sql":"DELETE FROM sales.orders"}`, 400},
		{"/api/clusters/1/rows/query", `{"sql":"DELETE FROM sales.orders","mode":"explain_analyze"}`, 400},
		{"/api/clusters/1/rows/query", `{"sql":"DELETE FROM sales.orders","skip":20}`, 400},
		{"/api/clusters/1/rows/query", `{"sql":"DROP TABLE sales.orders"}`, 400},
		{"/api/clusters/1/rows/query", `{"sql":"DELETE FROM sales.orders","allow_dml":true}`, 400},
		{"/api/clusters/1/rows/query", `{"sql":"DELETE FROM sales.orders WHERE id=1"}`, 200},
	} {
		before := reader.calls
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatal(tc.body, w.Code, w.Body.String())
		}
		if tc.status == 400 && reader.calls != before {
			t.Fatal("invalid SQL opened connection")
		}
		if tc.status == 200 && (!strings.Contains(w.Body.String(), `"affected_rows":"0"`) || reader.query.SQL != "DELETE FROM sales.orders WHERE id=1") {
			t.Fatal(w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/clusters", nil))
	if !strings.Contains(w.Body.String(), `"allow_dml":true`) || !strings.Contains(w.Body.String(), `"type":"tidb"`) {
		t.Fatal(w.Body.String())
	}
}

func TestDDLHTTPConnectionPolicy(t *testing.T) {
	reader := &queryRecorder{}
	h := New(config.Config{Clusters: []config.Cluster{{Name: "readonly"}, {Name: "dml", AllowDML: true}, {Name: "ddl", AllowDDL: true}}}, reader).Handler()
	for _, tc := range []struct {
		id, sql, extra string
		status         int
	}{
		{"0", "DROP TABLE sales.t", "", 400},
		{"1", "DROP TABLE sales.t", "", 400},
		{"2", "DELETE FROM sales.t", "", 400},
		{"2", "ALTER TABLE sales.t ADD x int", "", 200},
		{"2", "ALTER TABLE sales.t ADD x int", `,"mode":"explain_analyze"`, 400},
		{"2", "ALTER TABLE sales.t ADD x int; DROP TABLE sales.t", "", 400},
		{"0", "DROP TABLE sales.t", `,"allow_ddl":true`, 400},
	} {
		before := reader.calls
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/clusters/"+tc.id+"/rows/query", strings.NewReader(`{"database":"sales","sql":"`+tc.sql+`"`+tc.extra+`}`)))
		if w.Code != tc.status {
			t.Fatal(tc, w.Code, w.Body.String())
		}
		if tc.status == 400 && reader.calls != before {
			t.Fatal("invalid DDL opened connection")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/clusters", nil))
	if !strings.Contains(w.Body.String(), `"allow_ddl":true`) || !strings.Contains(w.Body.String(), `"allow_ddl":false`) {
		t.Fatal(w.Body.String())
	}
}
