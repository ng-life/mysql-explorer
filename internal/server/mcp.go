package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ng-life/mysql-explorer/internal/mysql"
)

type connectionInput struct {
	Connection string `json:"connection,omitempty" jsonschema:"配置中的连接名称；仅一个连接时可省略"`
}
type schemaInput struct {
	Connection string `json:"connection,omitempty" jsonschema:"配置中的连接名称"`
	Schema     string `json:"schema" jsonschema:"数据库/schema 名称"`
}
type describeInput struct {
	Connection string `json:"connection,omitempty" jsonschema:"配置中的连接名称"`
	Schema     string `json:"schema" jsonschema:"数据库/schema 名称"`
	Table      string `json:"table" jsonschema:"表名"`
	Mode       string `json:"mode,omitempty" jsonschema:"fields（默认）、indexes 或 ddl"`
}
type sqlInput struct {
	Connection string `json:"connection,omitempty" jsonschema:"配置中的连接名称"`
	Schema     string `json:"schema,omitempty" jsonschema:"默认数据库；表及索引 DDL 必须指定"`
	SQL        string `json:"sql" jsonschema:"单条 SELECT 或连接配置允许的 DML/DDL，不含 EXPLAIN 前缀"`
	Mode       string `json:"mode,omitempty" jsonschema:"query（默认）、explain 或 explain_analyze；ANALYZE 实际执行 SELECT"`
	Limit      int    `json:"limit,omitempty" jsonschema:"SELECT 每页行数，默认 20，范围 1–200；不追加到执行计划或 DML/DDL"`
	Skip       int    `json:"skip,omitempty" jsonschema:"SELECT 偏移量 0–1000000；计划和 DML/DDL 必须为 0"`
}

// Metadata structure methods are also used by CLI describe; keep the same
// implementation rather than reproducing information_schema SQL in the tools.
type structureReader interface {
	TableInfos(context.Context, string) ([]mysql.TableInfo, error)
	Fields(context.Context, string, string) ([]mysql.Field, error)
	Indexes(context.Context, string, string) ([]mysql.Index, error)
	DDL(context.Context, string, string) (string, error)
}

func (s *Server) mcpReader(ctx context.Context, name string) (mysql.Reader, error) {
	id, err := s.connectionID(name)
	if err != nil {
		return nil, err
	}
	reader, err := s.pool.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("无法连接数据库，请检查配置、认证和网络")
	}
	return reader, nil
}
func metadataError() error {
	return fmt.Errorf("元数据读取失败，请检查数据库、表名、权限或超时；数据库详细错误已隐藏")
}

func (s *Server) mcpHandler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "mysql-explorer", Version: "dev"}, &mcp.ServerOptions{Instructions: "先用 list_connections 查看 description 用途说明和连接能力，选择符合任务的连接。SQL 单语句；DML/DDL 默认关闭，仅由服务配置开启。EXPLAIN ANALYZE 实际执行查询。写入失败时不得自动重试，先核对数据库状态。"})
	readTool := func(name, description string) *mcp.Tool {
		return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
	}
	mcp.AddTool(server, readTool("list_connections", "列出已配置连接的名称、description 用途说明、地址、类型和 DML/DDL 开关，供模型判断连接用途，不显示密码，不连接数据库。"), func(ctx context.Context, _ *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		rows := []map[string]any{}
		for _, cl := range s.config.Clusters {
			kind := cl.Type
			if kind == "" {
				kind = "mysql"
			}
			rows = append(rows, map[string]any{"name": cl.Name, "description": cl.Description, "address": cl.Address, "type": kind, "allow_dml": cl.AllowDML, "allow_ddl": cl.AllowDDL})
		}
		return nil, map[string]any{"connections": rows}, nil
	})
	mcp.AddTool(server, readTool("list_schemas", "列出目标连接中账号可见的数据库。"), func(ctx context.Context, _ *mcp.CallToolRequest, in connectionInput) (*mcp.CallToolResult, any, error) {
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		reader, err := s.mcpReader(ctx, in.Connection)
		if err != nil {
			return nil, nil, err
		}
		names, err := reader.Databases(ctx)
		if err != nil {
			return nil, nil, metadataError()
		}
		return nil, map[string]any{"schemas": names}, nil
	})
	mcp.AddTool(server, readTool("list_tables", "列出数据库的表和视图名称、类型及备注。"), func(ctx context.Context, _ *mcp.CallToolRequest, in schemaInput) (*mcp.CallToolResult, any, error) {
		if !validName(in.Schema) {
			return nil, nil, fmt.Errorf("必须指定有效的 schema")
		}
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		reader, err := s.mcpReader(ctx, in.Connection)
		if err != nil {
			return nil, nil, err
		}
		names, err := reader.Databases(ctx)
		if err != nil {
			return nil, nil, metadataError()
		}
		visible := false
		for _, name := range names {
			visible = visible || name == in.Schema
		}
		if !visible {
			return nil, nil, fmt.Errorf("schema 不存在或不可见")
		}
		structure, ok := reader.(structureReader)
		if !ok {
			return nil, nil, metadataError()
		}
		tables, err := structure.TableInfos(ctx, in.Schema)
		if err != nil {
			return nil, nil, metadataError()
		}
		return nil, map[string]any{"tables": tables}, nil
	})
	mcp.AddTool(server, readTool("describe_table", "读取字段结构、索引或建表定义；mode=fields、indexes、ddl，读取 DDL 不执行结构变更。"), func(ctx context.Context, _ *mcp.CallToolRequest, in describeInput) (*mcp.CallToolResult, any, error) {
		if !validName(in.Schema) || !validName(in.Table) {
			return nil, nil, fmt.Errorf("必须指定有效的 schema 和 table")
		}
		if in.Mode != "" && in.Mode != "fields" && in.Mode != "indexes" && in.Mode != "ddl" {
			return nil, nil, fmt.Errorf("mode 必须为 fields、indexes 或 ddl")
		}
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		reader, err := s.mcpReader(ctx, in.Connection)
		if err != nil {
			return nil, nil, err
		}
		structure, ok := reader.(structureReader)
		if !ok {
			return nil, nil, metadataError()
		}
		fields, err := structure.Fields(ctx, in.Schema, in.Table)
		if err != nil {
			return nil, nil, metadataError()
		}
		switch in.Mode {
		case "indexes":
			indexes, err := structure.Indexes(ctx, in.Schema, in.Table)
			if err != nil {
				return nil, nil, metadataError()
			}
			return nil, map[string]any{"indexes": indexes}, nil
		case "ddl":
			ddl, err := structure.DDL(ctx, in.Schema, in.Table)
			if err != nil {
				return nil, nil, metadataError()
			}
			return nil, map[string]any{"ddl": ddl}, nil
		default:
			return nil, map[string]any{"fields": fields}, nil
		}
	})
	destructive := true
	mcp.AddTool(server, &mcp.Tool{Name: "execute_sql", Description: "执行 SELECT、EXPLAIN、EXPLAIN ANALYZE 或连接配置允许的单条 DML/DDL。ANALYZE 会实际执行 SELECT；写入可能提交，DDL 可能隐式提交。分页不限制写入范围，写入失败或超时后禁止自动重试，应先核对状态。", Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive}}, func(ctx context.Context, _ *mcp.CallToolRequest, in sqlInput) (*mcp.CallToolResult, any, error) {
		id, err := s.connectionID(in.Connection)
		if err != nil {
			return nil, nil, err
		}
		out, err := s.runQuery(ctx, id, queryRequest{Database: in.Schema, SQL: in.SQL, Mode: in.Mode, Limit: in.Limit, Skip: in.Skip})
		if err != nil {
			return nil, nil, err
		}
		return nil, out, nil
	})
	mcp.AddTool(server, readTool("get_metadata", "读取服务已加载的库、表、字段和备注缓存。"), func(ctx context.Context, _ *mcp.CallToolRequest, in connectionInput) (*mcp.CallToolResult, any, error) {
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		reader, err := s.mcpReader(ctx, in.Connection)
		if err != nil {
			return nil, nil, err
		}
		catalog := reader.Metadata()
		if catalog == nil {
			return nil, nil, fmt.Errorf("元数据未加载，请使用 refresh_metadata")
		}
		return nil, catalog, nil
	})
	mcp.AddTool(server, readTool("refresh_metadata", "重新读取服务的完整库、表、字段和备注缓存，超时 45 秒；不修改数据库。"), func(ctx context.Context, _ *mcp.CallToolRequest, in connectionInput) (*mcp.CallToolResult, any, error) {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		reader, err := s.mcpReader(ctx, in.Connection)
		if err != nil {
			return nil, nil, err
		}
		catalog, err := reader.Refresh(ctx)
		if err != nil {
			return nil, nil, metadataError()
		}
		return nil, catalog, nil
	})
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject browser-origin cross-site requests before the SDK creates a session.
		// Loopback-bound servers additionally reject foreign Host headers (DNS rebinding).
		host, _, _ := net.SplitHostPort(s.config.Listen)
		if host == "localhost" || net.ParseIP(host).IsLoopback() {
			requestHost := r.Host
			if h, _, err := net.SplitHostPort(r.Host); err == nil {
				requestHost = h
			}
			if requestHost != "localhost" && !net.ParseIP(strings.Trim(requestHost, "[]")).IsLoopback() {
				http.Error(w, "MCP Host 不允许", 403)
				return
			}
		}
		if origins := r.Header.Values("Origin"); len(origins) > 0 {
			origin, err := url.Parse(origins[0])
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if len(origins) != 1 || err != nil || origin.Scheme != scheme || !strings.EqualFold(origin.Host, r.Host) || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
				http.Error(w, "MCP Origin 不允许", 403)
				return
			}
		}
		if r.Method == http.MethodPost {
			// Bound and read the envelope here: the compatible SDK maps body
			// read failures to 500; oversized requests should receive HTTP 413.
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
			r.Body.Close()
			if err != nil {
				var limit *http.MaxBytesError
				if errors.As(err, &limit) {
					http.Error(w, "MCP 请求不超过 1 MiB", http.StatusRequestEntityTooLarge)
				} else {
					http.Error(w, "MCP 请求读取失败", 400)
				}
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		transport.ServeHTTP(w, r)
	})
}
