# 项目协作指南

## 基本约定

- 使用中文沟通、编写项目说明和用户可见文案。
- 按专业软件工程标准编写代码，保持格式统一；为安全边界、复杂逻辑和非直观约束添加必要注释，避免重复解释代码本身。
- Python 项目优先使用 uv，Node.js 项目优先使用 pnpm，Java 项目优先使用 Maven。
- 新建原型基于 Ant Design Pro 设计；本项目现有页面采用嵌入式 HTML 和 JavaScript，常规修改沿用现有结构。
- 修改前检查 Git 状态，保留已有未提交改动；控制修改范围，不顺带进行无关重构。

## 项目概况与目录

MySQL Explorer 是使用 Go 实现的轻量 MySQL 只读浏览器，支持多个服务、元数据缓存、SQL 编辑和分页查询。HTML 与编辑器 bundle 嵌入二进制，运行时无需 Node.js 或外部 CDN。

- `cmd/mysql-explorer/main.go`：程序入口、配置加载和退出信号处理。
- `internal/config/`：命令行参数、默认配置路径与 TOML 配置。
- `internal/cli/`：只读 CLI、输出格式和 zsh/Bash 动态补全。
- `internal/mysql/`：连接、元数据缓存、SQL AST 校验、分页和结果转换。
- `internal/server/`：HTTP 路由、请求校验、超时及错误响应。
- `internal/web/assets.go`：静态资源嵌入。
- `internal/web/assets/index.html`：浏览器页面、样式和交互。
- `frontend/editor.js`：CodeMirror 编辑器和 SQL 格式化源码。
- `internal/web/assets/editor.js`：生成的编辑器 bundle，修改源码后重新构建，不手工编辑。
- `config.example.toml`：可提交的配置示例。
- `.github/workflows/release.yml`：标签触发的验证与二进制发布流程。

## 开发命令

后端要求 Go 1.23 或更高版本；编辑器构建使用 Node.js 24，pnpm 版本遵循 `package.json` 的 `packageManager`。

```sh
# 后端测试与静态检查
make check

# 构建到 dist/mysql-explorer
make build

# 格式化 Go 代码
make fmt

# 安装锁定依赖并重建编辑器
make web

# 使用本地配置运行
go run ./cmd/mysql-explorer --config config.toml
```

首次运行从 `config.example.toml` 复制为 `config.toml`，通过配置所指定的环境变量提供密码。默认访问地址为 `http://127.0.0.1:18084`。不要将真实密码写入文档、测试或提交内容。

## 必须保持的行为与安全边界

- 保持只读定位：仅接受通过 AST 校验的单条 SELECT，在只读事务中执行。保留函数白名单、锁定查询拒绝和执行注释清除机制，不绕过校验直接执行原始 SQL。
- 保留分页限制、查询超时和结果大小限制；原 SQL 的 LIMIT/OFFSET 与应用分页组合语义以现有实现和测试为准。
- 整数与 DECIMAL 使用字符串返回，避免浏览器精度丢失；保留 NULL、二进制、日期时间及同名结果列的现有表示。
- 字段备注映射应保持结果列顺序，无法确定来源时回退为结果字段名。
- 各服务独立维护元数据缓存；保持启动预加载、展开数据库更新表名、选择表更新字段和备注、刷新完整元数据的行为。读取失败时保留旧缓存。
- 浏览器只接收公开连接信息；HTTP 错误不得暴露密码、DSN 或驱动原始错误。
- 本地配置、环境变量文件和构建产物遵循 `.gitignore`。应用无内置登录，默认监听回环地址；涉及远程访问的说明应保留认证代理或网络访问控制要求。

## 修改与验证

- Go 修改使用 `gofmt` 或 `make fmt`；涉及后端行为时运行 `make check`，交付前按改动范围运行 `make build`。
- 修复或新增查询、分页、字段映射及 HTTP 行为时，在对应包的 `*_test.go` 中补充必要回归测试，沿用现有测试模式。
- 修改 `frontend/editor.js` 或前端依赖后运行 `make web`，同步提交源码、`pnpm-lock.yaml`、生成的 bundle 及构建产生的许可证文件。发布 CI 会重建并检查生成文件一致性。
- 页面交互变化应在真实浏览器中验证相关流程，例如表选择、补全、格式化撤销、查询执行、翻页和错误提示。
- 明确区分单元测试、构建验证、浏览器验证和真实 MySQL 联调；没有执行的检查如实说明，不将 mock 测试或页面加载成功描述为真实数据库联调成功。
- 行为、配置或开发流程发生变化时同步更新 `README.md`；只有明确要求发布时才推送 `v*` 标签，标签会触发 GitHub Release。
