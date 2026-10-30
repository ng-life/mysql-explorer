package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ng-life/mysql-explorer/internal/config"
	"github.com/ng-life/mysql-explorer/internal/mysql"
)

type mcpFake struct {
	fake
	mu          sync.Mutex
	connections int
	queries     []mysql.Query
	fail        bool
}

func (f *mcpFake) Get(context.Context, int) (mysql.Reader, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connections++
	return f, nil
}
func (f *mcpFake) Execute(ctx context.Context, _ string, q mysql.Query) (mysql.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	if _, ok := ctx.Deadline(); !ok {
		return mysql.Result{}, errors.New("missing deadline")
	}
	if f.fail {
		return mysql.Result{}, errors.New("private-password raw driver error")
	}
	return mysql.Result{Rows: [][]any{{"9007199254740993"}}, Columns: []mysql.Column{{Name: "id"}}}, nil
}
func (f *mcpFake) TableInfos(context.Context, string) ([]mysql.TableInfo, error) {
	return []mysql.TableInfo{{Name: "orders", Type: "BASE TABLE", Comment: "订单"}}, nil
}
func (f *mcpFake) Fields(context.Context, string, string) ([]mysql.Field, error) {
	return []mysql.Field{{Name: "id", Type: "bigint", Position: 1}}, nil
}
func (f *mcpFake) Indexes(context.Context, string, string) ([]mysql.Index, error) {
	return []mysql.Index{{Name: "PRIMARY", Unique: true}}, nil
}
func (f *mcpFake) DDL(context.Context, string, string) (string, error) {
	return "CREATE TABLE orders(id bigint)", nil
}

func TestMCPStreamableHTTPTools(t *testing.T) {
	reader := &mcpFake{}
	cfg := config.Config{Listen: "127.0.0.1:0", Clusters: []config.Cluster{
		{Name: "readonly", Description: "订单只读核对", Address: "localhost:3306", Password: "private-password"},
		{Name: "dml", AllowDML: true}, {Name: "ddl", AllowDDL: true},
	}}
	srv := httptest.NewServer(New(cfg, reader).Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 7 {
		t.Fatal("tool count", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if tool.Name == "execute_sql" && (tool.Annotations.ReadOnlyHint || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint) {
			t.Fatal("write-capable tool advertised as read-only")
		}
	}
	call := func(name string, args map[string]any, wantError bool) *mcp.CallToolResult {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(name, err)
		}
		if result.IsError != wantError {
			t.Fatalf("%s: error=%t result=%+v", name, result.IsError, result)
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "private-password") {
			t.Fatal("credential/driver error leak")
		}
		return result
	}
	result := call("list_connections", map[string]any{}, false)
	raw, _ := json.Marshal(result.StructuredContent)
	if !strings.Contains(string(raw), `"allow_ddl":true`) || !strings.Contains(string(raw), `"description":"订单只读核对"`) {
		t.Fatal(string(raw))
	}
	reader.mu.Lock()
	count := reader.connections
	reader.mu.Unlock()
	if count != 0 {
		t.Fatal("connections must be offline")
	}
	call("list_schemas", map[string]any{"connection": "readonly"}, false)
	call("list_tables", map[string]any{"connection": "readonly", "schema": "shop"}, false)
	for _, mode := range []string{"fields", "indexes", "ddl"} {
		call("describe_table", map[string]any{"connection": "readonly", "schema": "shop", "table": "orders", "mode": mode}, false)
	}
	call("get_metadata", map[string]any{"connection": "readonly"}, false)
	call("refresh_metadata", map[string]any{"connection": "readonly"}, false)
	for _, mode := range []string{"query", "explain", "explain_analyze"} {
		result := call("execute_sql", map[string]any{"connection": "readonly", "sql": "SELECT 1", "mode": mode}, false)
		raw, _ := json.Marshal(result.StructuredContent)
		if !strings.Contains(string(raw), "9007199254740993") {
			t.Fatal(string(raw))
		}
		reader.mu.Lock()
		q := reader.queries[len(reader.queries)-1]
		reader.mu.Unlock()
		if q.Mode != mode || q.Limit != 20 {
			t.Fatal(q)
		}
	}
	for _, args := range []map[string]any{
		{"sql": "SELECT 1"},
		{"connection": "readonly", "sql": "DELETE FROM shop.orders"},
		{"connection": "readonly", "sql": "DROP TABLE shop.orders"},
		{"connection": "readonly", "sql": "SELECT 1; DELETE FROM shop.orders"},
		{"connection": "readonly", "sql": "SELECT 1", "limit": 201},
		{"connection": "readonly", "sql": "SELECT 1", "mode": "explain", "skip": 1},
		{"connection": "dml", "sql": "DROP TABLE shop.orders"},
		{"connection": "ddl", "sql": "DELETE FROM shop.orders"},
	} {
		reader.mu.Lock()
		before := reader.connections
		reader.mu.Unlock()
		call("execute_sql", args, true)
		reader.mu.Lock()
		after := reader.connections
		reader.mu.Unlock()
		if before != after {
			t.Fatal("invalid call opened database")
		}
	}
	reader.mu.Lock()
	beforeUnknown := reader.connections
	reader.mu.Unlock()
	_, unknownErr := session.CallTool(ctx, &mcp.CallToolParams{Name: "execute_sql", Arguments: map[string]any{"connection": "readonly", "sql": "DELETE FROM shop.orders", "allow_dml": true}})
	if unknownErr == nil || !strings.Contains(unknownErr.Error(), "additional properties") {
		t.Fatal("unknown write flag accepted", unknownErr)
	}
	reader.mu.Lock()
	afterUnknown := reader.connections
	reader.mu.Unlock()
	if beforeUnknown != afterUnknown {
		t.Fatal("unknown flag opened connection")
	}
	call("execute_sql", map[string]any{"connection": "dml", "schema": "shop", "sql": "DELETE FROM orders WHERE id=1"}, false)
	call("execute_sql", map[string]any{"connection": "ddl", "schema": "shop", "sql": "ALTER TABLE orders ADD value int"}, false)
	reader.mu.Lock()
	reader.fail = true
	reader.mu.Unlock()
	call("execute_sql", map[string]any{"connection": "readonly", "sql": "SELECT 1"}, true)
	call("execute_sql", map[string]any{"connection": "dml", "sql": "DELETE FROM shop.orders"}, true)
}

func TestMCPHTTPGuards(t *testing.T) {
	h := New(config.Config{Listen: "127.0.0.1:18084"}, fake{}).Handler()
	for _, tc := range []struct {
		host, origin string
		want         int
	}{
		{"127.0.0.1:18084", "https://evil.example", 403},
		{"127.0.0.1:18084", "null", 403},
		{"evil.example:18084", "", 403},
		{"127.0.0.1:18084", "http://127.0.0.1:18084", 405},
		{"localhost:18084", "", 405},
	} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/mcp", nil)
		r.Host = tc.host
		r.Header.Set("Accept", "text/event-stream")
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:18084/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"`+strings.Repeat("x", 1024*1024)+`","version":"1"},"capabilities":{}}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("oversized request must be rejected", w.Code)
	}
}

func TestStartupInstructions(t *testing.T) {
	text := startupInstructions("0.0.0.0:18084")
	for _, want := range []string{"http://127.0.0.1:18084/", "http://127.0.0.1:18084/mcp", "Streamable HTTP", "mcpServers", "connections", "allow_ddl"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing startup guidance", want)
		}
	}
	if !strings.Contains(startupInstructions("[::1]:19000"), "http://[::1]:19000/mcp") {
		t.Fatal("IPv6 URL invalid")
	}
}
