# MySQL Explorer

轻量 Go MySQL 只读浏览器，参考 [Mongo Explorer](https://github.com/ng-life/mongo-explorer) 的界面与按需加载方式。

- 切换服务，展开数据库后加载表与视图。
- 查询表数据，查看字段类型、主键、NULL 属性及完整行内容；支持复制和放大查看。
- JSON 等值筛选，字段升降序排序，每页 20、50、100、200 条。
- 后端生成参数化 SELECT，不接收任意 SQL，不提供新增、修改或删除接口。数据查询在只读事务内运行。

## 运行

需要 Go 1.23 或更高版本。

```sh
cp config.example.toml config.toml
export MYSQL_EXPLORER_PASSWORD='your-password'
go run ./cmd/mysql-explorer --config config.toml
```

打开 <http://127.0.0.1:18084>。默认监听回环地址；HTML 嵌入可执行文件，无需 Node.js 或额外静态资源。

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

筛选仅支持字段的等值条件和 NULL，不接受表达式、操作符或 SQL：

```json
{"status":"active","deleted_at":null}
```

排序值为 `1`（升序）或 `-1`（降序）：

```json
{"created_at":-1}
```

默认按主键升序排序；指定排序时追加主键作为同值排序的判定字段。无主键且未指定排序的表不保证顺序。多字段排序按字段名排列。翻页使用已执行的筛选条件，修改输入后需再次查询。

整数与 DECIMAL 以字符串返回，防止 JavaScript 精度丢失；筛选大整数和精确小数时也请使用 JSON 字符串，例如 `{"id":"9007199254740993"}`。NULL 返回 JSON null；二进制字段显示为 `{"base64":"..."}`。日期时间保留数据库文本格式。

每页最多 200 条，多读一条判断下一页，不执行全量计数。偏移量上限 1000000，HTTP 请求超时 12 秒，每页 JSON 行内容上限 32 MiB；单个大字段仍需先由驱动读取。分页采用 OFFSET，在并发数据变化时不保证跨页快照一致。仅支持数据库可用权限范围内的表；服务器需支持只读事务。

## 验证与发布

```sh
make check
make build
```

测试覆盖 SQL 参数化、标识符转义、筛选与排序校验、精确数字、HTTP 写入口拒绝和凭据隐藏。推送 `v*` 标签后，GitHub Actions 验证并发布 Linux x86_64 与 macOS arm64 二进制。

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
