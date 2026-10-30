package server

import (
	"encoding/json"
	"net"
	"strings"
)

func startupInstructions(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "服务地址无效"
	}
	if host == "" || net.ParseIP(host).IsUnspecified() {
		host = "127.0.0.1"
	}
	base := "http://" + net.JoinHostPort(host, port)
	snippet, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"mysql-explorer": map[string]string{"type": "http", "url": base + "/mcp"}}})
	return strings.Join([]string{
		"MySQL Explorer 已启动，监听 " + address,
		"Web 页面：" + base + "/",
		"MCP 地址：" + base + "/mcp（Streamable HTTP，与 Web 共用端口）",
		"MCP 接入：客户端选择 Streamable HTTP，填写上述 /mcp 地址；不是旧版 /sse，也无需启动独立 MCP 进程。",
		"常见 mcpServers 配置示例（字段按客户端要求调整）：" + string(snippet),
		"CLI 示例：mysql-explorer connections；mysql-explorer query --connection <连接名> --sql 'SELECT 1'",
		"权限：HTTP 页面、CLI、MCP 均遵循连接 type、allow_dml、allow_ddl；DML/DDL 默认关闭。",
		"地址中的 127.0.0.1 仅供本机访问；远程接入使用服务器地址及认证代理/网络访问控制，服务无内置登录。",
	}, "\n")
}
