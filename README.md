# MySQL Explorer

轻量 Go MySQL / TiDB 数据浏览器，默认只读，参考 [Mongo Explorer](https://github.com/ng-life/mongo-explorer) 的界面与按需加载方式。

- 启动时按服务预加载账号可见的数据库、表、字段和备注，各服务独立缓存。
- 展开数据库时检查表名更新；点击表时检查字段及备注更新，再执行默认查询。
- 「格式化」按钮整理当前 SQL 的缩进、换行和关键字大小写，支持撤销，不触发查询。
- CodeMirror 6 提供 SQL 高亮、关键字补全、已加载表名和选中表字段补全；Ctrl+Space 手动补全，Ctrl/⌘+Enter 执行。
- 选中表时填入 `SELECT * FROM db_name.table_name`，支持编辑 SQL、分页和快捷键执行。
- 查询结果使用动态列，优先显示来源字段备注，无备注则显示结果字段名。
- 支持普通 SELECT、WHERE、ORDER BY、聚合及 JOIN；表别名和字段别名按 SQL 投影位置映射。
- 默认仅支持单条 SELECT；连接可显式启用 TiDB 兼容和 DML。默认拒绝 DDL，始终拒绝锁定 SELECT、文件导出和未知存储函数。

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

可增加多个 `[[clusters]]`。支持 `password` 或 `password_env`，后者覆盖前者。配置文件已加入 Git 忽略规则；浏览器只接收服务名称、主机端口和连接能力配置，错误响应不包含驱动原始错误或凭据。应用无内置登录，远程访问应由现有网络访问控制或认证代理保护；只读连接应使用数据库只读账号，写入连接按需授予最小 DML 权限。

## Streamable HTTP MCP

无子命令启动或执行 `mysql-explorer serve` 时，在同一监听地址提供网页和 MCP：

- Web：`http://127.0.0.1:18084/`
- MCP：`http://127.0.0.1:18084/mcp`
- 传输选择 **Streamable HTTP**，不是旧版 HTTP+SSE 的 `/sse`。无需运行独立 MCP 进程。

启动日志输出实际 Web/MCP 地址、客户端接入示例和 CLI 用法。网页右下角固定图标打开「MCP 接入配置」弹窗，提供可复制的地址及常见配置示例，按当前网页地址生成 URL。不同客户端的配置键可能不同，以下以支持 `mcpServers`、`type: http`、`url` 的格式为例：

```json
{
  "mcpServers": {
    "mysql-explorer": {
      "type": "http",
      "url": "http://127.0.0.1:18084/mcp"
    }
  }
}
```

远程客户端需将主机改为服务器地址，并确保 `listen` 绑定到所需的可达地址。服务无内置登录，不应直接暴露公网；使用认证代理或网络访问控制。反向代理需转发 `/mcp` 的请求与 MCP 协议头；MCP 校验 Origin，默认回环监听同时校验 Host。无 Origin 的原生 MCP 客户端可以连接；不启用跨站 CORS。

| MCP 工具 | 对应功能与参数 |
| --- | --- |
| `list_connections` | CLI `connections`；不连接数据库，返回名称、description 用途说明、地址、type、allow_dml、allow_ddl |
| `list_schemas` | CLI `schemas`；参数 `connection` |
| `list_tables` | CLI `tables`；参数 `connection`、`schema`，返回名称、类型和备注 |
| `describe_table` | CLI `describe`；参数 `connection`、`schema`、`table`、`mode`（fields / indexes / ddl） |
| `execute_sql` | CLI `query`；参数 `connection`、`schema`、`sql`、`mode`（query / explain / explain_analyze）、`limit`、`skip` |
| `get_metadata` | 网页已加载的数据库/表/字段备注缓存；参数 `connection` |
| `refresh_metadata` | 网页刷新完整元数据；参数 `connection`，超时 45 秒 |

连接名称需要精确匹配；只有一个连接时可省略 `connection`。多个连接时不自动选择第一个。`execute_sql` 不通过 HTTP 回环调用，直接使用 HTTP API 共用的校验/执行层及同一连接池。SELECT 默认每页 20 行，最多 200 行，偏移量上限 1000000；SQL 执行超时 12 秒，返回行内容上限 32 MiB。MCP HTTP 请求体上限 1 MiB，包含 JSON-RPC 包装。

MCP 沿用 type、allow_dml、allow_ddl、AST 校验和数据库权限，不允许工具参数临时开启写权限。执行计划不追加分页，DML/DDL 不支持分页，EXPLAIN ANALYZE 实际执行 SELECT。写入可能提交，DDL 可能隐式提交；失败或超时不得自动重试，应先核对状态。结果保留有序 `columns` 和二维 `rows`，大整数与 DECIMAL 保留字符串。

MCP 使用官方 Go SDK v1.3.1，保持 Go 1.23 兼容。采用无状态 Streamable HTTP，POST 返回 JSON，不维护跨请求会话，不提供旧版 SSE 入口或 GET 事件流；直接用浏览器打开 `/mcp` 不等于 MCP 客户端握手。

## 连接类型与写入权限

每个 `[[clusters]]` 独立配置，Web 和 CLI 使用同一策略。Web 的 SERVICES 列表显示每个连接的 MySQL / TiDB 类型与 DML、DDL 开启/关闭标签；CLI 的 `connections` 表格显示「类型」「允许 DML」「允许 DDL」，JSON 输出包含 `type` 字符串和 `allow_dml`、`allow_ddl` 布尔字段。修改配置后需重启 Web 服务；CLI 下次运行读取新配置。

```toml
[[clusters]]
name = "TiDB 示例"
description = "订单分析数据库，用于订单查询和执行计划分析"
type = "tidb"
address = "127.0.0.1:4000"
user = "readonly"
password_env = "TIDB_EXPLORER_PASSWORD"
allow_dml = false
allow_ddl = false
```

| 配置项 | 默认值 | 行为 |
| --- | --- | --- |
| `description` | `""` | 连接用途说明；CLI connections 表格/JSON 和 MCP list_connections 返回，网页不展示，也不通过网页连接 API 返回 |
| `type` | `"mysql"` | 仅接受 `"mysql"`、`"tidb"`；不根据端口、版本自动切换 |
| `allow_dml` | `false` | 启用后允许单条 INSERT、REPLACE、UPDATE、DELETE；适用于两种数据库类型 |
| `allow_ddl` | `false` | 独立允许表、索引和数据库 DDL，不会启用 DML |

MySQL 的 SELECT 和执行计划继续使用只读事务。TiDB 未实现同等的 READ ONLY 事务保护；显式指定 `type = "tidb"` 后，SELECT 和执行计划使用普通事务，读取结束后回滚，仍执行 AST 校验和函数白名单。TiDB 的只读保障需要数据库账号权限配合；程序不修改 `tidb_enable_noop_functions`，也不会因 MySQL 只读事务失败自动降级。

启用 `allow_dml = true` 后，Web 的「执行 SQL」和 CLI 的 `query` 可执行 DML。每次只执行一条，成功后提交，失败时尝试回滚。应用分页不限制 DML 影响范围；需要限定范围时在 SQL 中明确编写 WHERE 或受支持的 LIMIT。CLI 不接受 DML 的 `--limit`、`--offset`；HTTP 的 DML `skip` 必须为 0，`limit` 不追加到 DML。执行计划仅接受 SELECT，即使允许 DML，也不允许通过 EXPLAIN ANALYZE 执行写入。

DML 响应保留 `columns`、`rows`、`has_more`，并增加字符串字段 `affected_rows`；INSERT / REPLACE 还返回字符串字段 `last_insert_id`，无自增值时通常为 `"0"`。DML 不分页。Web 显示提交结果，CLI 默认显示影响行数及自增 ID；`--format json` 返回原始结构。

```sh
# 仅对配置中已启用 allow_dml 的连接生效；此命令会修改数据
mysql-explorer query --connection 写入环境 --schema sales \
  --sql "UPDATE orders SET status = 'closed' WHERE id = 123" --format json
```

应用不自动重试 DML。超时、断连或提交失败后，实际提交结果可能不明确，应先核对数据库状态；非事务表或触发器等数据库行为也可能影响回滚边界。启用 DML 前应配置访问控制，并限制数据库账号的写入权限。

### DDL 开关

在需要结构变更的连接中设置 `allow_ddl = true`。Web 使用「执行 SQL」，CLI 使用 `query --sql` 或 `--file`；不支持分页和 EXPLAIN。可执行当前解析器支持的 CREATE TABLE / INDEX / DATABASE（SCHEMA）、ALTER TABLE、DROP TABLE / INDEX / DATABASE、TRUNCATE 和 RENAME TABLE。表和索引 DDL 必须选择数据库，CLI 使用 `--schema`，即使 SQL 中的表名已带库名。

DDL 使用完整原始定义执行，避免旧解析器重新生成 SQL 时丢失 ALTER 子句或索引定义。程序会检查开关、支持的语句类别和单语句边界；数据库负责最终语法和权限校验。DDL 不接受 SQL 注释、CREATE SELECT、临时表、视图、存储过程、函数、触发器和事件；SELECT/DML 仍沿用 AST 重写、注释清除和函数白名单。

DDL 可能隐式提交，不能保证回滚。程序不为 DDL 包装事务，不自动重试；失败、断连或超时后应先核对结构状态。执行结果的 `statement_type` 为 `ddl`，DML 为 `dml`，查询结果不包含该字段。DDL 成功后点击刷新元数据，更新库、表和字段列表。Web 和 CLI `connections` 均展示 DDL 开关；配置开关不替代数据库权限。

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

SELECT 使用 AST 校验；配置启用 DML 后，DML 也经过同一函数白名单和单语句校验。常用聚合、字符串、日期和 JSON 函数可用；未知函数、锁定查询和文件导出会被拒绝。当前不支持 CTE 和窗口函数等部分 MySQL 8 语法。解析后重新生成 SQL，移除注释，避免执行注释绕过校验。

每页最多 200 条，自动设置 LIMIT/OFFSET，多读一条判断下一页。原 SQL 的 LIMIT 是结果总数上限，分页保留原 OFFSET；翻页使用已执行的 SQL，修改编辑框后需点击执行查询。无 ORDER BY 时不保证结果顺序，并发变化时不保证跨页快照一致。偏移量上限 1000000，请求超时 12 秒，每页行内容上限 32 MiB；单个大字段仍需先由驱动读取。

整数与 DECIMAL 以字符串返回，防止浏览器丢失精度。NULL 单独显示，二进制显示 Base64，日期时间保留数据库文本格式。MySQL 服务器需要支持只读事务，TiDB 使用上文的兼容路径。

## SQL 执行计划

在编辑器输入单条 SELECT，点击「EXPLAIN」查看表格执行计划，点击「EXPLAIN ANALYZE」查看实际执行计划；MySQL 显示文本树，TiDB 显示多列表格。无需手动添加 EXPLAIN 前缀。

- MySQL 的 EXPLAIN 使用 `FORMAT=TRADITIONAL`；TiDB 使用默认 EXPLAIN 格式，均显示表格。
- EXPLAIN ANALYZE 会实际执行 SELECT，需要 MySQL 8.0.18+ 或支持该语法的 TiDB；数据库版本和查询类型不支持时显示错误，不自动回退或重复执行。
- 分析保留原 SQL 的 LIMIT/OFFSET，不追加应用分页，页面的每页条数不影响执行计划。分析大表时可先自行限制 SQL。
- 沿用 SELECT AST 校验、函数白名单、注释清除、按连接类型选择的事务、12 秒请求超时和 32 MiB 返回内容限制。执行计划不分页，返回完整计划。
- Web API：`POST /api/clusters/{id}/rows/query` 支持 `mode` 为 `query`（默认）、`explain` 或 `explain_analyze`；SQL 字段仍传入 SELECT，计划模式的 `skip` 必须为 0。响应沿用 `columns`、`rows`、`has_more`，计划的 `has_more` 为 false。

## 结果表格

查询结果、EXPLAIN 和 TiDB EXPLAIN ANALYZE 使用 Tabulator；MySQL 的单列 ANALYZE 计划仍使用文本树。宽表使用表格底部的原生横向滚动条，显式样式让它在内容溢出时常驻显示，不随滚动停止而隐藏。表格按字段类型对齐：整数、小数等数值列（含 UNSIGNED、带精度的声明）右对齐，文本、时间、二进制列左对齐；判断依据是列元数据而不是单元格内容，因此形如 "12.5" 的字符串列仍左对齐。表格支持拖动列边界调整宽度、拖动表头换序、「显示列」切换列、框选单元格后复制。双击单元格、选中后按 Enter 或点击「查看单元格」，可查看并复制长文本全文。

表格仅浏览数据，不提供单元格编辑或粘贴写入；SQL 写入仍通过连接的 DML/DDL 配置和执行入口。分页继续由后端控制，不自动加载全部数据；当前不启用表头排序和本地过滤，完整结果排序、筛选应写入 SQL。复制仅包含当前页选区，无表头；NULL 显示为 NULL，空字符串显示为双引号，大整数和 DECIMAL 保留字符串，同名列按位置独立展示。

Tabulator JS/CSS 和 MIT 许可证均打包嵌入二进制，无需 CDN。许可证可从 `/assets/result-grid.LICENSE.txt` 查看。

## 前端开发

修改 `frontend/editor.js`、`frontend/result-grid.js`、`frontend/result-grid.css` 或前端依赖后，使用 Node.js 24 和 pnpm 重新构建：

```sh
make web
```

前端源代码、依赖锁文件和生成的 JS/CSS、许可证都提交到仓库。发布 CI 会重建并检查静态产物一致性；浏览器无需访问外部 CDN。

启动预加载通过 SHOW DATABASES 和两次 information_schema 批量查询完成，不扫描表数据。每个服务超时 45 秒，全部启动尝试结束后开放 HTTP；失败服务显示未加载状态，其他服务正常可用。补全直接使用全部预加载元数据，无需先展开表。

展开数据库时更新该数据库的表名缓存，点击表时更新字段和备注缓存，刷新按钮重新加载当前服务的完整元数据。查询失败时保留旧缓存；并发 DDL 下不保证多次元数据读取属于同一数据库快照。

## 验证与发布

```sh
make check
make build
```

测试覆盖连接类型、DML 开关及提交/回滚、TiDB 计划列、SELECT 校验、危险语句拒绝、LIMIT 分页、字段备注来源解析、同名列与整数精度、HTTP 校验和凭据隐藏。推送 `v*` 标签后，GitHub Actions 验证并发布 Linux x86_64 与 macOS arm64 二进制。

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

## 命令行查询与元数据

同一个二进制支持 Web 与一次性 CLI 命令。无子命令时仍启动 Web，也可使用 `mysql-explorer serve`。CLI 直接连接配置中的目标服务，不需要 Web 服务运行，不预加载所有服务的元数据。

MySQL 的 database 与 schema 是同义词；命令层级为「已配置连接 → schema → 表 → 表结构」。所有命令支持 `--config`，可放在子命令前或后。默认优先使用当前目录的 `config.toml`，否则读取 `$XDG_CONFIG_HOME/mysql-explorer/config.toml`；未设置 `XDG_CONFIG_HOME` 时读取 `~/.config/mysql-explorer/config.toml`。

```sh
# 列出配置中的连接，不连接数据库，不显示密码
mysql-explorer connections

# 列出账号可见的数据库/schema
mysql-explorer schemas --connection 开发环境

# 列出表和视图的名称、类型、备注
mysql-explorer tables --connection 开发环境 --schema sales

# 字段顺序、类型、可空、默认值、键、额外属性、备注
mysql-explorer describe --connection 开发环境 --schema sales --table orders

# 索引及完整建表定义，两种选项不能同时使用
mysql-explorer describe --connection 开发环境 --schema sales --table orders --indexes
mysql-explorer describe --connection 开发环境 --schema sales --table orders --ddl

# 执行单条 SELECT；默认每页 100 行，最多 200 行
mysql-explorer query --connection 开发环境 --schema sales \
  --sql 'SELECT id, amount FROM orders ORDER BY id DESC' --limit 100 --offset 0

# 文件或标准输入，输入上限 1 MiB
mysql-explorer query --connection 开发环境 --schema sales --file query.sql
cat query.sql | mysql-explorer query --connection 开发环境 --schema sales --file -

# JSON 输出，适合脚本处理
mysql-explorer query --connection 开发环境 \
  --sql 'SELECT * FROM sales.orders' --format json

# 估算执行计划（表格）
mysql-explorer query --connection 开发环境 --schema sales \
  --mode explain --sql 'SELECT * FROM orders WHERE id = 1'

# 实际执行并分析（MySQL 8.0.18+）；终端保留文本树缩进
mysql-explorer query --connection 开发环境 --schema sales \
  --mode explain_analyze --file query.sql

# JSON 输出完整计划，沿用 columns / rows / has_more
mysql-explorer query --connection 开发环境 --schema sales \
  --mode explain_analyze --file query.sql --format json

mysql-explorer query --help
```

只有一个连接时可省略 `--connection`；多个连接时必须通过名称精确选择，名称重复时报错。`tables` 和 `describe` 必须指定 `--schema`；`query` 可省略，此时 SQL 中的表需使用完整名称。`--sql` 和 `--file` 必须且只能指定一个；只有显式使用 `--file -` 才读取标准输入。

默认使用 Lip Gloss 渲染圆角表格和彩色表头，中文按终端显示宽度对齐。长内容自动换行，窄终端在多列无法容纳时按记录纵向展示字段和值；空结果显示「无数据」。重定向输出及设置 `NO_COLOR` 时不输出 ANSI 样式。控制字符使用转义形式，NULL 显示为 `NULL`，空字符串显示为 `""`。所有 CLI 数据命令支持 `--format json`，JSON 保留原始值。查询 JSON 沿用 Web 的 `columns`、二维数组 `rows` 和 `has_more`，不会因列名重复覆盖数据。表格查询输出也包含行数及 `has_more`；当前页不是完整结果时，使用 `--offset` 继续查询。

`query --mode` 支持 `query`（默认）、`explain`、`explain_analyze`。计划模式复用 SQL 文件和标准输入，保留 SQL 本身的 LIMIT/OFFSET，不接受 `--limit` 或 `--offset`。EXPLAIN ANALYZE 会实际执行 SELECT，不自动重试或回退；版本或查询类型不支持时返回退出码 1。表格模式下 EXPLAIN 显示表格，EXPLAIN ANALYZE 在 MySQL 下显示文本树，TiDB 下显示多列表格；JSON 不附加提示或分页摘要。

CLI 沿用连接级 SQL 策略、函数白名单和事务规则；SELECT 保留分页上限、32 MiB 行内容限制和 12 秒请求超时。SQL 原有 LIMIT/OFFSET 与 CLI 分页按 Web 相同规则组合，不自动读取全部结果。字段结构 JSON 的 `default: null` 与 `default: ""` 分别表示 SQL NULL 与空字符串默认值。

数据写入 stdout，参数或运行错误写入 stderr；数据库原始错误不直接输出。退出码：`0` 成功（包括空结果），`1` 配置、连接、读取或输出失败，`2` 参数或 SQL 校验失败。帮助命令不加载配置、不连接数据库。


## Shell 动态补全

支持 zsh 和 Bash（包括 macOS 自带 Bash 3）。补全命令、选项、执行模式、输出格式和文件路径；`--connection` 从当前配置读取连接名称，`--schema` 与 `--table` 根据已选连接/schema 实时查询账号可见的元数据。支持空格分隔与 `--schema=value` 等写法。

```sh
# zsh：在 compinit 或 Oh My Zsh 初始化之后加载
mkdir -p ~/.local/share/mysql-explorer
mysql-explorer completion zsh > ~/.local/share/mysql-explorer/completion.zsh
source ~/.local/share/mysql-explorer/completion.zsh

# Bash：在交互式 shell 的启动文件中加载
mysql-explorer completion bash > ~/.local/share/mysql-explorer/completion.bash
source ~/.local/share/mysql-explorer/completion.bash
```

将对应 `source` 行加入 `~/.zshrc` 或 Bash 启动文件即可持久启用。在以下位置按 Tab：

```text
mysql-explorer schemas --connection <Tab>
mysql-explorer tables --connection TMS_RELEASE --schema <Tab>
mysql-explorer describe --connection TMS_RELEASE --schema sc_setting --table <Tab>
```

动态补全只读取元数据，不执行 SQL、不启动 Web、不预加载其他服务。每次补全重新读取配置和相关元数据，不保存跨命令缓存；数据库补全超时为 2 秒。配置、连接或网络不可用时安静返回空候选，不显示数据库详细错误。连接有多个时，补全 schema 前需选定连接；补全表名前需选定 schema。`--sql` 输入不提供 SQL 文本补全。

## 本机安装

```sh
make build
mkdir -p ~/.local/bin ~/.config/mysql-explorer
install -m 755 dist/mysql-explorer ~/.local/bin/mysql-explorer
# 将配置放到 ~/.config/mysql-explorer/config.toml，并设置权限 600
chmod 600 ~/.config/mysql-explorer/config.toml
```

确保 `~/.local/bin` 在 PATH 中。安装后可在任意目录执行 CLI；当前目录存在 `config.toml` 时仍优先使用它，也可通过 `--config` 指定其他文件。
