package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ng-life/mysql-explorer/internal/config"
	"github.com/ng-life/mysql-explorer/internal/mysql"
)

func TestParse(t *testing.T) {
	for _, args := range [][]string{{"--config", "query", "schemas", "--connection", "serve"}, {"schemas", "--config=query", "--connection=serve"}} {
		o, err := parse(args, &bytes.Buffer{})
		if err != nil || o.config != "query" || o.command != "schemas" || o.connection != "serve" {
			t.Fatalf("%+v %v", o, err)
		}
	}
	for _, args := range [][]string{{"query"}, {"query", "--sql", "select 1", "--file", "-"}, {"query", "--sql", "select 1", "--limit", "201"}, {"tables"}, {"describe", "--schema", "s", "--table", "t", "--indexes", "--ddl"}, {"connections", "--connection", "x"}, {"query", "--sql", "select 1", "unexpected"}, {"query", "--sql", "select 1", "--", "ignored"}} {
		if _, err := parse(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	o, err := parse([]string{"--listen", "127.0.0.1:18085"}, &bytes.Buffer{})
	if err != nil || o.command != "serve" || o.listen != "127.0.0.1:18085" {
		t.Fatal(o, err)
	}
}

func TestHelpAndValidationWithoutDatabase(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"query", "--help"}, {"describe", "--help"}} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &errs); code != 0 {
			t.Fatal(code, errs.String())
		}
	}
	for _, sql := range []string{"DELETE FROM t", "SELECT 1; SELECT 2", "SELECT * FROM t FOR UPDATE"} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), []string{"query", "--sql", sql}, strings.NewReader(""), &out, &errs); code != 2 || out.Len() != 0 {
			t.Fatal(code, out.String(), errs.String())
		}
	}
	var out, errs bytes.Buffer
	code := Run(context.Background(), []string{"query", "--file", "-"}, strings.NewReader("DELETE FROM t"), &out, &errs)
	if code != 2 {
		t.Fatal(code, errs.String())
	}
}

func TestConnectionsOfflineAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte("[[clusters]]\nname='offline'\naddress='127.0.0.1:1'\nuser='readonly'\npassword='top-secret'\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := Run(context.Background(), []string{"connections", "--config", path, "--format", "json"}, strings.NewReader(""), &out, &errs); code != 0 {
		t.Fatal(code, errs.String())
	}
	if !strings.Contains(out.String(), "readonly") || strings.Contains(out.String(), "top-secret") {
		t.Fatal(out.String())
	}
	out.Reset()
	errs.Reset()
	if code := Run(context.Background(), []string{"schemas", "--config", path}, strings.NewReader(""), &out, &errs); code != 1 || strings.Contains(errs.String(), "top-secret") {
		t.Fatal(code, errs.String())
	}
}

func TestConnectionSelection(t *testing.T) {
	c := config.Config{Clusters: []config.Cluster{{Name: "a"}, {Name: "b"}}}
	if _, err := selectConnection(c, ""); err == nil {
		t.Fatal("ambiguous default")
	}
	if id, err := selectConnection(c, "b"); err != nil || id != 1 {
		t.Fatal(id, err)
	}
	c.Clusters[1].Name = "a"
	if _, err := selectConnection(c, "a"); err == nil {
		t.Fatal("duplicate")
	}
	if _, err := selectConnection(c, "missing"); err == nil {
		t.Fatal("missing")
	}
	c.Clusters = c.Clusters[:1]
	if id, err := selectConnection(c, ""); err != nil || id != 0 {
		t.Fatal(id, err)
	}
}

func TestOutputPreservesColumnsAndEscapesControls(t *testing.T) {
	var out, errs bytes.Buffer
	rows := [][]any{{"9007199254740993", "\x1b[31m\n\t", nil, ""}}
	if code := output("table", &out, &errs, nil, []string{"id\x1b[31m", "id", "null", "empty"}, rows); code != 0 {
		t.Fatal(code)
	}
	if strings.ContainsAny(out.String(), "\x1b") || !strings.Contains(out.String(), `\x1b[31m\n\t`) || !strings.Contains(out.String(), "9007199254740993") || !strings.Contains(out.String(), `""`) {
		t.Fatal(out.String())
	}
}

func TestOversizedInputAndJSONControlPreservation(t *testing.T) {
	var out, errs bytes.Buffer
	if code := Run(context.Background(), []string{"query", "--file", "-"}, strings.NewReader(strings.Repeat("x", 1024*1024+1)), &out, &errs); code != 1 {
		t.Fatal(code)
	}
	out.Reset()
	errs.Reset()
	if code := output("json", &out, &errs, [][]any{{"a\nb\x1b"}}, nil, nil); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), `a\nb\u001b`) {
		t.Fatal(out.String())
	}
}

func TestPlanOptionsAndOfflineValidation(t *testing.T) {
	for _, mode := range []string{"explain", "explain_analyze"} {
		o, err := parse([]string{"--config", "query", "query", "--mode=" + mode, "--file", "-", "--format", "json"}, &bytes.Buffer{})
		if err != nil || o.mode != mode || o.config != "query" {
			t.Fatal(o, err)
		}
		for _, extra := range [][]string{{"--limit", "100"}, {"--offset=0"}} {
			args := append([]string{"query", "--mode", mode, "--sql", "SELECT 1"}, extra...)
			if _, err := parse(args, &bytes.Buffer{}); err == nil {
				t.Fatal("accepted plan pagination", args)
			}
		}
		for _, statement := range []string{"DELETE FROM t", "SELECT 1; SELECT 2", "SELECT * FROM t FOR UPDATE", "SELECT sleep(10)"} {
			var out, errs bytes.Buffer
			code := Run(context.Background(), []string{"query", "--mode", mode, "--config", "/nonexistent/config", "--file", "-"}, strings.NewReader(statement), &out, &errs)
			if code != 2 || out.Len() != 0 {
				t.Fatal(code, out.String(), errs.String())
			}
		}
	}
	var out, errs bytes.Buffer
	if code := Run(context.Background(), []string{"query", "--mode", "invalid", "--sql", "SELECT 1"}, strings.NewReader(""), &out, &errs); code != 2 {
		t.Fatal(code)
	}
}

type planReader struct {
	mysql.Reader
	query    mysql.Query
	database string
	err      error
}

func (r *planReader) Execute(_ context.Context, database string, q mysql.Query) (mysql.Result, error) {
	r.query, r.database = q, database
	return mysql.Result{Columns: []mysql.Column{{Name: "EXPLAIN"}}, Rows: [][]any{{"-> scan\n    -> rows\x1b[31m"}}}, r.err
}
func TestCLIPlanDispatchAndOutput(t *testing.T) {
	for _, mode := range []string{"query", "explain", "explain_analyze"} {
		for _, format := range []string{"table", "json"} {
			t.Run(mode+"/"+format, func(t *testing.T) {
				var out, errs bytes.Buffer
				reader := &planReader{}
				o := options{mode: mode, format: format, statement: "SELECT * FROM orders LIMIT 42", schema: "sales", limit: 100}
				if code := executeQuery(context.Background(), reader, o, &out, &errs); code != 0 || errs.Len() != 0 {
					t.Fatal(code, errs.String())
				}
				if reader.query.Mode != mode || reader.query.SQL != o.statement || reader.database != "sales" {
					t.Fatal(reader.query)
				}
				if format == "json" {
					var result mysql.Result
					if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Rows[0][0] != "-> scan\n    -> rows\x1b[31m" {
						t.Fatal(out.String(), err)
					}
				} else {
					if strings.ContainsRune(out.String(), '\x1b') {
						t.Fatal("terminal escape leaked")
					}
					if mode == "explain_analyze" && !strings.Contains(out.String(), "-> scan\n    -> rows") {
						t.Fatal("tree line breaks lost", out.String())
					}
					if mode != "query" && strings.Contains(out.String(), "has_more") {
						t.Fatal("plan has pagination footer", out.String())
					}
				}
			})
		}
	}
	var out, errs bytes.Buffer
	if code := executeQuery(context.Background(), &planReader{err: fmt.Errorf("private-password")}, options{mode: "explain_analyze"}, &out, &errs); code != 1 || out.Len() != 0 || strings.Contains(errs.String(), "private-password") {
		t.Fatal(code, out.String(), errs.String())
	}
}

func TestCLIDMLConfigValidation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "config.toml")
		body := fmt.Sprintf("[[clusters]]\nname='offline'\naddress='127.0.0.1:1'\nuser='reader'\nallow_dml=%t\n", enabled)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		args := []string{"query", "--config", path, "--sql", "DELETE FROM sales.orders WHERE id=1"}
		if enabled {
			args = append(args, "--limit", "20")
		}
		if code := Run(context.Background(), args, strings.NewReader(""), &out, &errs); code != 2 || out.Len() != 0 {
			t.Fatal(code, out.String(), errs.String())
		}
		want := "allow_dml"
		if enabled {
			want = "不支持 --limit"
		}
		if !strings.Contains(errs.String(), want) {
			t.Fatal(errs.String())
		}
	}
}

type dmlResultReader struct {
	mysql.Reader
	failure bool
}

func (r dmlResultReader) Execute(context.Context, string, mysql.Query) (mysql.Result, error) {
	if r.failure {
		return mysql.Result{}, fmt.Errorf("private-password")
	}
	n, id := "2", "9007199254740993"
	return mysql.Result{Rows: [][]any{}, Columns: []mysql.Column{}, AffectedRows: &n, LastInsertID: &id}, nil
}
func TestCLIDMLResult(t *testing.T) {
	for _, format := range []string{"table", "json"} {
		var out, errs bytes.Buffer
		o := options{mode: "query", format: format, statement: "INSERT INTO sales.orders(id) VALUES(1)"}
		if code := executeQuery(context.Background(), dmlResultReader{}, o, &out, &errs); code != 0 {
			t.Fatal(code, errs.String())
		}
		if format == "json" {
			var result mysql.Result
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.AffectedRows == nil || *result.AffectedRows != "2" || *result.LastInsertID != "9007199254740993" {
				t.Fatal(out.String(), err)
			}
		} else if !strings.Contains(out.String(), "影响行数") || strings.Contains(out.String(), "has_more") {
			t.Fatal(out.String())
		}
		out.Reset()
		errs.Reset()
		if code := executeQuery(context.Background(), dmlResultReader{failure: true}, o, &out, &errs); code != 1 || out.Len() != 0 || !strings.Contains(errs.String(), "不要直接重试") || strings.Contains(errs.String(), "private-password") {
			t.Fatal(code, out.String(), errs.String())
		}
	}
}
