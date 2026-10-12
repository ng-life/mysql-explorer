# MySQL Explorer

轻量 Go MySQL 只读浏览器，参考 [Mongo Explorer](https://github.com/ng-life/mongo-explorer) 的界面与按需加载方式。

- 启动时按服务预加载账号可见的数据库、表、字段和备注，各服务独立缓存。
- 展开数据库时检查表名更新；点击表时检查字段及备注更新，再执行默认查询。
- 「格式化」按钮整理当前 SQL 的缩进、换行和关键字大小写，支持撤销，不触发查询。
- CodeMirror 6 提供 SQL 高亮、关键字补全、已加载表名和选中表字段补全；Ctrl+Space 手动补全，Ctrl/⌘+Enter 执行。
- 选中表时填入 `SELECT * FROM db_name.table_name`，支持编辑 SQL、分页和快捷键执行。
- 查询结果使用动态列，优先显示来源字段备注，无备注则显示结果字段名。
- 支持普通 SELECT、WHERE、ORDER BY、聚合及 JOIN；表别名和字段别名按 SQL 投影位置映射。
- 仅支持单条 SELECT，在只读事务中执行；不提供数据写入、锁定查询、文件导出或任意存储函数调用。

## 运行

需要 Go 1.23 或更高版本。

```sh
cp config.example.toml config.toml
export MYSQL_EXPLORER_PASSWORD='your-password'
go run ./cmd/mysql-explorer --config config.toml
```

打开 <http://127.0.0.1:18084>。默认监听回环地址；HTML 嵌入可执行文件，运行无需 Node.js 或额外静态资源，CodeMirror 及其依赖已打包并嵌入程序。

```toml
listen = "127.0.0.1:18084"

[[clusters]]
name = "开发环境"
address = "127.0.0.1:3306"
user = "readonly"
password_env = "MYSQL_EXPLORER_PASSWORD"
```

可增加多个 `[[clusters]]`。支持 `password` 或 `password_env`，后者覆盖前者。配置文件已加入 Git 忽略规则；浏览器只接收服务名称和主机端口，错误响应不包含驱动原始错误或凭据。应用无内置登录，远程访问应由现有网络访问控制或认证代理保护，并使用数据库只读账号。

## 查询

选择表会自动填入并执行默认查询，也可以直接输入带库名的 SQL。展开数据库后，未限定库名的表在该数据库中查询。

```sql
SELECT * FROM sales.orders
```

```sql
SELECT o.id AS order_no, c.name AS customer_name, o.amount
FROM sales.orders o
JOIN sales.customers c ON o.customer_id = c.id
WHERE o.status = 'active'
ORDER BY o.id DESC
```

结果列按 SQL 返回顺序显示。直接字段、字段别名、`*`、`table.*` 和普通 JOIN 会解析来源表的 `COLUMN_COMMENT`。无备注、表达式、UNION 或无法确定来源的列显示结果字段名；派生表及 JOIN USING 等复杂投影可能回退为字段名。同名列独立保留，不覆盖数据。

SQL 使用 AST 校验，只接收单条 SELECT。常用聚合、字符串、日期和 JSON 函数可用；未知函数、锁定查询和文件导出会被拒绝。当前不支持 CTE 和窗口函数等部分 MySQL 8 语法。解析后重新生成 SQL，移除注释，避免执行注释绕过校验。

每页最多 200 条，自动设置 LIMIT/OFFSET，多读一条判断下一页。原 SQL 的 LIMIT 是结果总数上限，分页保留原 OFFSET；翻页使用已执行的 SQL，修改编辑框后需点击执行查询。无 ORDER BY 时不保证结果顺序，并发变化时不保证跨页快照一致。偏移量上限 1000000，请求超时 12 秒，每页行内容上限 32 MiB；单个大字段仍需先由驱动读取。

整数与 DECIMAL 以字符串返回，防止浏览器丢失精度。NULL 单独显示，二进制显示 Base64，日期时间保留数据库文本格式。服务器需要支持只读事务。

## 编辑器开发

修改 `frontend/editor.js` 后使用 Node.js 24 和 pnpm 重新构建：

```sh
make web
```

编辑器源代码、依赖锁文件和生成的 bundle 都提交到仓库。发布 CI 会重建并检查 bundle 一致性；浏览器无需访问外部 CDN。

启动预加载通过 SHOW DATABASES 和两次 information_schema 批量查询完成，不扫描表数据。每个服务超时 45 秒，全部启动尝试结束后开放 HTTP；失败服务显示未加载状态，其他服务正常可用。补全直接使用全部预加载元数据，无需先展开表。

展开数据库时更新该数据库的表名缓存，点击表时更新字段和备注缓存，刷新按钮重新加载当前服务的完整元数据。查询失败时保留旧缓存；并发 DDL 下不保证多次元数据读取属于同一数据库快照。

## 验证与发布

```sh
make check
make build
```

测试覆盖 SELECT 校验、危险语句拒绝、LIMIT 分页、字段备注来源解析、同名列与整数精度、HTTP 校验和凭据隐藏。推送 `v*` 标签后，GitHub Actions 验证并发布 Linux x86_64 与 macOS arm64 二进制。

## Ubuntu systemd

将二进制放到 `~/.local/bin/mysql-explorer`，配置放到 `~/.config/mysql-explorer/config.toml`（权限 `600`）。用户服务示例：

```ini
[Unit]
Description=MySQL Explorer read-only web browser
After=network-online.target

[Service]
ExecStart=%h/.local/bin/mysql-explorer --config %h/.config/mysql-explorer/config.toml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now mysql-explorer
```
