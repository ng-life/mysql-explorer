// Package cli provides one-shot read-only commands alongside the HTTP server.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ng-life/mysql-explorer/internal/config"
	"github.com/ng-life/mysql-explorer/internal/mysql"
	"github.com/ng-life/mysql-explorer/internal/server"
)

const help = `用法：mysql-explorer [--config config.toml] <命令> [选项]
命令：serve、connections、schemas、tables、describe、query、completion
省略命令时启动 Web 服务。使用 <命令> --help 查看选项。
`

type options struct {
	command, config, listen, connection, schema, table, statement, file, format string
	limit, offset                                                               int
	indexes, ddl                                                                bool
}

// parse separates a command from flags while allowing --config on either side.
// Flag values are consumed before detecting commands, including values named "query".
func parse(args []string, stderr io.Writer) (options, error) {
	o := options{command: "serve"}
	rest := []string{}
	found := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			if found {
				return o, fmt.Errorf("不支持位置参数")
			}
			o.command = a
			found = true
			continue
		}
		rest = append(rest, a)
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if !strings.Contains(a, "=") && name != "help" && name != "h" && name != "indexes" && name != "ddl" {
			if i+1 >= len(args) {
				return o, fmt.Errorf("选项缺少值")
			}
			i++
			rest = append(rest, args[i])
		}
	}
	fs := flag.NewFlagSet(o.command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, help); fs.PrintDefaults() }
	fs.StringVar(&o.config, "config", config.DefaultPath(), "TOML 配置文件")
	switch o.command {
	case "serve":
		fs.StringVar(&o.listen, "listen", "", "HTTP 监听地址")
	case "connections", "schemas", "tables", "describe", "query":
		fs.StringVar(&o.format, "format", "table", "输出格式：table 或 json")
		if o.command != "connections" {
			fs.StringVar(&o.connection, "connection", "", "配置中的服务名称")
		}
		if o.command == "tables" || o.command == "describe" || o.command == "query" {
			fs.StringVar(&o.schema, "schema", "", "数据库/schema 名称")
		}
		if o.command == "describe" {
			fs.StringVar(&o.table, "table", "", "表名")
			fs.BoolVar(&o.indexes, "indexes", false, "查看索引")
			fs.BoolVar(&o.ddl, "ddl", false, "查看建表定义")
		}
		if o.command == "query" {
			fs.StringVar(&o.statement, "sql", "", "单条 SELECT")
			fs.StringVar(&o.file, "file", "", "SQL 文件，- 表示标准输入")
			fs.IntVar(&o.limit, "limit", 100, "每页行数，1–200")
			fs.IntVar(&o.offset, "offset", 0, "偏移量，0–1000000")
		}
	default:
		return o, fmt.Errorf("未知命令")
	}
	if err := fs.Parse(rest); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("不支持位置参数")
	}
	if o.command != "serve" && o.format != "table" && o.format != "json" {
		return o, fmt.Errorf("输出格式必须是 table 或 json")
	}
	if (o.command == "tables" || o.command == "describe") && o.schema == "" {
		return o, fmt.Errorf("必须指定 --schema")
	}
	if o.command == "describe" && (o.table == "" || o.indexes && o.ddl) {
		return o, fmt.Errorf("必须指定 --table，--indexes 与 --ddl 不能同时使用")
	}
	if o.command == "query" {
		if (o.statement == "") == (o.file == "") {
			return o, fmt.Errorf("必须且只能提供 --sql 或 --file")
		}
		if o.limit < 1 || o.limit > 200 || o.offset < 0 || o.offset > 1000000 {
			return o, fmt.Errorf("分页参数超出范围")
		}
	}
	return o, nil
}

func selectConnection(c config.Config, name string) (int, error) {
	if name == "" {
		if len(c.Clusters) == 1 {
			return 0, nil
		}
		return 0, fmt.Errorf("请使用 --connection 指定唯一的服务名称")
	}
	selected := -1
	for i, cl := range c.Clusters {
		if cl.Name == name {
			if selected >= 0 {
				return 0, fmt.Errorf("服务名称重复，请修改配置")
			}
			selected = i
		}
	}
	if selected < 0 {
		return 0, fmt.Errorf("未找到指定服务")
	}
	return selected, nil
}

// Run returns a process exit code and never prints raw driver errors or credentials.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "__complete" {
		return complete(ctx, args[1:], stdout)
	}
	if len(args) > 0 && args[0] == "completion" {
		return completionScript(args[1:], stdout, stderr)
	}
	o, err := parse(args, stderr)
	if err == flag.ErrHelp {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if o.command == "serve" {
		legacy := []string{"--config", o.config}
		if o.listen != "" {
			legacy = append(legacy, "--listen", o.listen)
		}
		c, err := config.Load(legacy)
		if err != nil {
			fmt.Fprintln(stderr, "无法加载服务配置")
			return 1
		}
		if err := server.Run(ctx, c); err != nil {
			fmt.Fprintln(stderr, "Web 服务启动或运行失败")
			return 1
		}
		return 0
	}
	// Validate SQL before opening any database connection.
	if o.command == "query" {
		if o.file != "" {
			var reader io.Reader = stdin
			var f *os.File
			if o.file != "-" {
				f, err = os.Open(o.file)
				if err != nil {
					fmt.Fprintln(stderr, "无法读取 SQL 文件")
					return 1
				}
				defer f.Close()
				reader = f
			}
			raw, readErr := io.ReadAll(io.LimitReader(reader, 1024*1024+1))
			if readErr != nil || len(raw) > 1024*1024 {
				fmt.Fprintln(stderr, "SQL 输入读取失败或超过 1 MiB")
				return 1
			}
			o.statement = string(raw)
		}
		if _, err := mysql.ParseSQL(o.statement); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	c, err := config.Read(o.config)
	if err != nil {
		fmt.Fprintln(stderr, "无法加载配置，请检查文件格式和连接参数")
		return 1
	}
	if o.command == "connections" {
		type connection struct {
			Name    string `json:"name"`
			Address string `json:"address"`
			User    string `json:"user"`
		}
		out := []connection{}
		rows := [][]any{}
		for _, cl := range c.Clusters {
			out = append(out, connection{cl.Name, cl.Address, cl.User})
			rows = append(rows, []any{cl.Name, cl.Address, cl.User})
		}
		return output(o.format, stdout, stderr, out, []string{"名称", "地址", "用户"}, rows)
	}
	id, err := selectConnection(c, o.connection)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	pool := mysql.NewPool(c.Clusters)
	defer pool.Close()
	reader, err := pool.Get(ctx, id)
	if err != nil {
		fmt.Fprintln(stderr, "无法连接 MySQL，请检查地址、认证和网络")
		return 1
	}
	client := reader.(*mysql.Client)
	var data any
	var headers []string
	rows := [][]any{}
	switch o.command {
	case "schemas":
		var names []string
		names, err = client.Databases(ctx)
		data = names
		headers = []string{"Schema"}
		for _, name := range names {
			rows = append(rows, []any{name})
		}
	case "tables":
		// Check visibility so a nonexistent schema is not mistaken for an empty one.
		var names []string
		names, err = client.Databases(ctx)
		if err == nil {
			visible := false
			for _, name := range names {
				visible = visible || name == o.schema
			}
			if !visible {
				err = fmt.Errorf("schema unavailable")
			}
		}
		if err == nil {
			var tables []mysql.TableInfo
			tables, err = client.TableInfos(ctx, o.schema)
			data = tables
			headers = []string{"名称", "类型", "备注"}
			for _, t := range tables {
				rows = append(rows, []any{t.Name, t.Type, t.Comment})
			}
		}
	case "describe":
		var fields []mysql.Field
		fields, err = client.Fields(ctx, o.schema, o.table)
		if err == nil {
			switch {
			case o.ddl:
				var ddl string
				ddl, err = client.DDL(ctx, o.schema, o.table)
				data = struct {
					DDL string `json:"ddl"`
				}{ddl}
				headers = []string{"DDL"}
				rows = append(rows, []any{ddl})
			case o.indexes:
				var indexes []mysql.Index
				indexes, err = client.Indexes(ctx, o.schema, o.table)
				data = indexes
				headers = []string{"索引", "唯一", "顺序", "字段", "前缀长度", "类型"}
				for _, idx := range indexes {
					rows = append(rows, []any{idx.Name, idx.Unique, idx.Position, nullableString(idx.Column), nullableInt(idx.Prefix), idx.Type})
				}
			default:
				data = fields
				headers = []string{"顺序", "字段", "类型", "可空", "默认值", "键", "额外属性", "备注"}
				for _, f := range fields {
					rows = append(rows, []any{f.Position, f.Name, f.Type, f.Nullable, nullableString(f.Default), f.Key, f.Extra, f.Comment})
				}
			}
		}
	case "query":
		var result mysql.Result
		result, err = client.Execute(ctx, o.schema, mysql.Query{SQL: o.statement, Limit: o.limit, Skip: o.offset})
		data = result
		rows = result.Rows
		for _, col := range result.Columns {
			headers = append(headers, col.Name)
		}
		if err == nil && o.format == "table" {
			if code := output(o.format, stdout, stderr, data, headers, rows); code != 0 {
				return code
			}
			_, err = fmt.Fprintln(stdout, tableFooter(stdout, len(rows), result.HasMore))
			if err != nil {
				fmt.Fprintln(stderr, "无法写入输出")
				return 1
			}
			return 0
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "读取失败，请检查 schema、表名、SQL、权限或超时；数据库详细错误已隐藏")
		return 1
	}
	return output(o.format, stdout, stderr, data, headers, rows)
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}
func nullableInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
func cell(v any) string {
	if v == nil {
		return "NULL"
	}
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case map[string]string:
		raw, _ := json.Marshal(x)
		s = string(raw)
	default:
		s = fmt.Sprint(v)
	}
	if s == "" {
		return `""`
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func output(format string, stdout, stderr io.Writer, data any, headers []string, rows [][]any) int {
	var err error
	if format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(data)
	} else {
		_, err = fmt.Fprintln(stdout, renderTable(stdout, headers, rows))
	}
	if err != nil {
		fmt.Fprintln(stderr, "无法写入输出")
		return 1
	}
	return 0
}
