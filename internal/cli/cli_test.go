package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ng-life/mysql-explorer/internal/config"
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
