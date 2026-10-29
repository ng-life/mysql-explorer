package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionWords(t *testing.T) {
	c := completionWords([]string{"--config", "query", "describe", "--connection=with spaces", "--schema", "s", "--table=ord"})
	if c.command != "describe" || c.values["config"] != "query" || c.values["connection"] != "with spaces" || c.values["schema"] != "s" || c.valueFlag != "table" || c.prefix != "ord" || c.equals != "--table=" {
		t.Fatalf("%+v", c)
	}
	c = completionWords([]string{"query", "--sql", "SELECT '--schema' FROM t", "--format", "j"})
	if c.valueFlag != "format" || c.values["sql"] != "SELECT '--schema' FROM t" {
		t.Fatalf("%+v", c)
	}
}

func TestStaticCompletion(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"query", "--mo"}, "--mode\n"},
		{[]string{"query", "--mode", "explain"}, "explain\nexplain_analyze\n"},
		{[]string{"query", "--mode=explain_a"}, "--mode=explain_analyze\n"},
		{[]string{"qu"}, "query\n"},
		{[]string{"describe", "--ta"}, "--table\n"},
		{[]string{"query", "--format", "j"}, "json\n"},
		{[]string{"query", "--format=j"}, "--format=json\n"},
		{[]string{"completion", "z"}, "zsh\n"},
		{[]string{"query", "--sql", "SELECT"}, ""},
	} {
		var out bytes.Buffer
		if code := complete(context.Background(), tc.args, &out); code != 0 || out.String() != tc.want {
			t.Fatalf("%v: %d %q", tc.args, code, out.String())
		}
	}
}

func TestDynamicConnectionsAndQuietFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte("[[clusters]]\nname='开发 环境'\naddress='127.0.0.1:1'\nuser='reader'\npassword='secret'\n[[clusters]]\nname='开发 环境'\naddress='127.0.0.1:1'\nuser='reader'\n[[clusters]]\nname='other'\naddress='127.0.0.1:1'\nuser='reader'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	complete(context.Background(), []string{"--config", p, "schemas", "--connection", "开发"}, &out)
	if out.String() != "开发 环境\n" {
		t.Fatal(out.String())
	}
	out.Reset()
	complete(context.Background(), []string{"--config", p, "tables", "--connection=开发"}, &out)
	if out.String() != "--connection=开发 环境\n" {
		t.Fatal(out.String())
	}
	out.Reset()
	complete(context.Background(), []string{"--config", p, "tables", "--connection", "other", "--schema", ""}, &out)
	if out.Len() != 0 {
		t.Fatal(out.String())
	}
	// Configuration changes must be visible on the next completion invocation.
	if err := os.WriteFile(p, []byte("[[clusters]]\nname='new'\naddress='127.0.0.1:1'\nuser='reader'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	complete(context.Background(), []string{"--config", p, "schemas", "--connection", ""}, &out)
	if out.String() != "new\n" {
		t.Fatal(out.String())
	}
}

func TestScriptsAndPrivateDispatch(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		var out, errs bytes.Buffer
		code := Run(context.Background(), []string{"completion", shell}, strings.NewReader(""), &out, &errs)
		if code != 0 || !strings.Contains(out.String(), "__complete") || errs.Len() != 0 {
			t.Fatal(code, errs.String())
		}
	}
	var out, errs bytes.Buffer
	if code := Run(context.Background(), []string{"__complete", "query", "--limit", ""}, strings.NewReader(""), &out, &errs); code != 0 || out.Len() != 0 || errs.Len() != 0 {
		t.Fatal(code, out.String(), errs.String())
	}
	if code := Run(context.Background(), []string{"completion", "fish"}, strings.NewReader(""), &out, &errs); code != 2 {
		t.Fatal(code)
	}
}
