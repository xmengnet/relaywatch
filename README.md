# RelayWatch

自托管的版本更新监控服务：网页配置监控上游、自动检测新版本、多渠道推送通知。

## 文档

- [需求文档](docs/requirements.md) — 功能范围、验收标准、里程碑
- [设计方案](docs/design.md) — 技术选型、数据模型、接口、调度、认证、安全
- [部署文档](docs/deploy.md) — Docker、systemd、反向代理、备份

## 定位

现有工具的空白：

- **nvchecker** 版本检测精确，但纯 CLI、配置手写 TOML、通知只有桌面弹窗
- **changedetection.io** 有 Web UI 和丰富推送，但只做网页文本 diff，不懂版本号语义，误报多
- **GitHub Watch** 只支持 GitHub，只发邮件

RelayWatch 把前者的精确版本检测和后者的 Web 操作体验结合起来。

## 计划中的核心能力

| 能力 | 说明 |
|---|---|
| 多源监控 | GitHub / GitLab / Gitea / AUR / Arch 官方 / PyPI / npm / crates / Go module / 通用 HTTP JSON / 正则抓取 / 容器镜像 |
| 版本语义比较 | semver + 非标准格式回退，支持前缀后缀剥离与 include/ignore 版本过滤 |
| 多渠道通知 | Telegram / Discord / Slack / ntfy / Gotify / SMTP / 飞书 / 企业微信 / 钉钉 / PushDeer / Server酱 / Bark / 通用 Webhook |
| 批量导入 | 兼容 nvchecker keyfile（TOML）批量导入，JSON 导出备份 |
| 认证 | 本地账户（bcrypt）+ 多 OIDC Provider（支持 Claim 限制）+ API Token |
| Web 界面 | Vue 3 + Vuetify SPA，构建产物 `go:embed` 进二进制，运行时零依赖 |
| 单二进制部署 | 纯 Go SQLite，无 cgo，无外部依赖 |

## 状态

M1–M5 已完成（验收冒烟通过），核心功能全部就绪。

**监控源**（13）：github_release / github_tag / gitlab_release / gitea_release / aur / arch_official / pypi / npm / crates / go_module / http_json / http_regex / docker_tag

**通知渠道**（13）：webhook / telegram / discord / slack / ntfy / gotify / email_smtp / feishu / wecom / dingtalk / pushdeer / serverchan / bark

已完成的能力：
- 认证：本地账户（bcrypt cost 12）、首次引导、会话 Cookie + CSRF、API Token（Bearer）、OIDC 登录（授权码 + PKCE、claim 规则、首次登录自动建档）、敏感字段 AES-256-GCM 加密（`secret_key` 强制、旧密钥过渡）
- 检查引擎：semver + 分段回退版本比较、include/ignore 过滤、五态状态机（unknown/up_to_date/has_update/error/missing，含版本回退语义）
- 调度器：ticker + 确定性抖动 + per-host 令牌桶限速 + 手动检查同步返回
- 通知系统：text/template 模板、投递队列（指数退避重试、静默期 deferred、补发前校验、ack/停用/重发作废）、渠道测试发送
- 导入 / 导出：nvchecker keyfile（TOML）预览导入；JSON 导出（默认脱敏）
- Web 界面：Vue 3 + Vuetify 4.3，动态源/渠道表单、监控列表与详情、通知历史、导入页、设置页、仪表盘
- 运维：`/metrics` Prometheus 文本指标、Dockerfile、systemd unit（[部署文档](docs/deploy.md)）

后续可选：SSE 实时推送、「上游 vs 本地」双轨对比（v2）。

## 构建与开发

```bash
make build        # pnpm build + go build，产出 ./relaywatch
./relaywatch -config config.example.yaml
# 浏览器打开 http://localhost:8080，首次访问引导创建管理员
```

**必须配置加密密钥**（未设置时拒绝启动）：`openssl rand -base64 32` 写入配置 `security.secret_key` 或环境变量 `RELAYWATCH_SECRET_KEY`。密钥丢失则加密的 token/webhook 配置无法恢复，请务必备份；轮换用 `security.old_secret_key` 过渡。

```bash
# 前端热更新开发
go run ./cmd/relaywatch              # 终端 1（:8080）
cd web && pnpm dev                   # 终端 2（:5173，API 代理到 8080）
```

环境要求：Go 1.27+、Node 22+、pnpm。前端依赖安装（pnpm 12）会提示 esbuild 构建脚本，package.json 的 `pnpm.onlyBuiltDependencies` 已放行。

**已知版本约束**：vue-tsc 3.3.13 与 TypeScript 7 不兼容（`ERR_PACKAGE_PATH_NOT_EXPORTED`），前端固定使用 TypeScript 5.9。

## 技术栈

**后端**：Go 1.27+ / chi v5 / modernc.org/sqlite（纯 Go，无 cgo）

**前端**：Vue 3.5 / Vuetify 4.3 / Vite 8 / TypeScript 5.9 / Pinia 4.1 / vue-router 5.4

**部署**：`make build` 先构建前端到 `web/dist`，再由 `go:embed` 打进二进制。用户拿到单个可执行文件，运行时零依赖。

详见[设计方案](docs/design.md)。
