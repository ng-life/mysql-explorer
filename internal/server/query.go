package server

import (
	"context"
	"fmt"
	"time"

	"github.com/ng-life/mysql-explorer/internal/mysql"
)

type queryError struct {
	status  int
	message string
}

func (e *queryError) Error() string { return e.message }

// runQuery is the single HTTP/MCP SQL boundary: validate connection permissions
// before opening a connection, apply the same limits, and hide driver details.
func (s *Server) runQuery(ctx context.Context, id int, in queryRequest) (mysql.Result, error) {
	fail := func(status int, message string) (mysql.Result, error) {
		return mysql.Result{}, &queryError{status, message}
	}
	if id < 0 || id >= len(s.config.Clusters) {
		return fail(404, "未知的 MySQL 服务")
	}
	if in.Database != "" && !validName(in.Database) {
		return fail(400, "数据库名称无效")
	}
	if len(in.SQL) > 1024*1024 {
		return fail(400, "SQL 不超过 1 MiB")
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit < 1 || in.Limit > 200 || in.Skip < 0 || in.Skip > 1000000 {
		return fail(400, "每页 1–200 条，偏移量 0–1000000")
	}
	q := mysql.Query{Mode: in.Mode, SQL: in.SQL, Limit: in.Limit, Skip: in.Skip}
	cl := s.config.Clusters[id]
	if err := mysql.ValidateQuery(q, cl.AllowDML, cl.AllowDDL); err != nil {
		return fail(400, err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client, err := s.pool.Get(ctx, id)
	if err != nil {
		return fail(502, "无法连接 MySQL，请检查地址、认证和网络后重试")
	}
	out, err := client.Execute(ctx, in.Database, q)
	if err != nil {
		message := "查询失败，请检查 SQL、数据库和权限；结果过大时减少每页条数"
		if in.Mode == "explain" || in.Mode == "explain_analyze" {
			message = "执行计划分析失败，请检查 SQL、权限和超时；请确认数据库支持当前执行计划语法"
		}
		if _, parseErr := mysql.ParseSQL(in.SQL); parseErr != nil {
			message = "DML/DDL 执行失败，结果可能未确定；请先核对数据库状态，不要直接重试"
		}
		return fail(502, message)
	}
	return out, nil
}

func (s *Server) connectionID(name string) (int, error) {
	if name == "" && len(s.config.Clusters) == 1 {
		return 0, nil
	}
	id := -1
	for i, cl := range s.config.Clusters {
		if name != "" && cl.Name == name {
			if id >= 0 {
				return 0, fmt.Errorf("服务名称重复，请修改配置")
			}
			id = i
		}
	}
	if id < 0 {
		return 0, fmt.Errorf("请通过 connection 指定唯一的服务名称")
	}
	return id, nil
}
