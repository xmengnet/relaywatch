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

## 核心能力

| 能力 | 说明 |
|---|---|
| 全生态多源监控（33+） | GitHub / GitLab / Gitea / Bitbucket / Pagure / 通用 Git (ls-remote) / AUR / Arch 官方 / Debian / Ubuntu / PyPI / npm / crates / Go / RubyGems / Maven / Packagist / CPAN / CRAN / Hackage / OCI 容器 (GHCR/Quay/Harbor) / VS Marketplace / Open VSX / Snapcraft / HTTP 正则 / HTTP JSON / HTTP Header 重定向 / 手动基线 / 受控本地命令 |
| 双轨对比（AUR vs 上游） | 将 AUR/本地打包指向对应上游参照，检测包落后与追平；智能对齐并剥离 epoch 与 pkgrel，防止误报 |
| 实时事件推送 | SSE 实时通道（`/api/v1/events`），长任务与版本检查完成无刷新即刻响应 |
| 高级版本重写与过滤 | semver 比较 + 非标分段比较，支持前缀后缀剥离、include/ignore 过滤与 `from_pattern`/`to_pattern` 正则重写流水线 |
| 多渠道通知 | Telegram / Discord / Slack / ntfy / Gotify / SMTP / 飞书 / 企业微信 / 钉钉 / PushDeer / Server酱 / Bark / 通用 Webhook |
| nvchecker 无损导入 | 100% 兼容 nvchecker keyfile（TOML）批量导入，pacman/alpm 透明代换为官方 REST API，JSON 导出备份 |
| 企业级认证与安全 | 本地账户（bcrypt）+ 多 OIDC Provider（自定义图标、Claim 限制）+ API Token，敏感字段 AES-256-GCM 加密 |
| Web 界面 | Vue 3 + Vuetify 4.3 SPA，单二进制 `go:embed` 嵌入，零外部依赖运行 |
| 单二进制部署 | 纯 Go SQLite（无 cgo，无外部依赖） |

## 状态

**M1–M8 全部完成并全量测试通过**：核心版本更新检测、双轨对比、SSE 实时事件、nvchecker 33 种监控源无损导入，以及多用户/权限、网站管理、帮助中心与中英双语界面均已就绪。

**监控源**（33）：
- **代码托管与 VCS**：`github_release` / `github_tag` / `gitlab_release` / `gitea_release` / `bitbucket` / `pagure` / `git_tag` (通用 Git)
- **编程语言包生态**：`pypi` / `npm` / `crates` / `go_module` / `rubygems` / `maven` / `packagist` / `cpan` / `cran` / `hackage`
- **版本追踪与上游聚合**：`repology` / `anitya`
- **Linux 发行版**：`aur` / `arch_official` / `debianpkg` / `ubuntupkg` (nvchecker `pacman`/`alpm` 自动映射代换)
- **容器镜像**：`container` (标准 OCI V2：GHCR / Quay / Harbor) / `docker_tag` (Docker Hub)
- **通用网络提取**：`http_regex` / `http_json` / `http_header` (重定向/HEAD)
- **生态市场与系统源**：`openvsx` / `vsmarketplace` / `snapcraft` / `manual` / `cmd` (受控沙箱)

**通知渠道**（13）：`webhook` / `telegram` / `discord` / `slack` / `ntfy` / `gotify` / `email_smtp` / `feishu` / `wecom` / `dingtalk` / `pushdeer` / `serverchan` / `bark`

已完成的能力：
- **双轨对比**：aur 自动剥离 epoch/pkgrel 对齐版本、entered_lagging/gap_grew/caught_up 状态机、通知管线复用、编辑页参照绑定、列表/详情/仪表盘全链路呈现
- **实时通信**：高并发 SSE 广播总线（`EventBroker`）、保活心跳、前端 `useSse` 响应式自动重连
- **版本流水线**：semver + 分段回退比较、include/ignore 过滤、`from_pattern`/`to_pattern` 正则重写（双向兼容 Python `\1` 与 Go `$1`）、五态状态机（含版本回退与 missing 态）
- **认证与安全**：本地账户（bcrypt）、首次引导、会话 Cookie + CSRF、API Token（Bearer）、OIDC Provider 管理（自定义图标与编辑）、敏感字段 AES-256-GCM 加密
- **调度与限速**：ticker + 确定性抖动 + per-host 令牌桶限速 + 优雅关停（在途任务排空落库）
- **通知编排**：text/template 模板、投递队列（指数退避、静默期 deferred、补发前校验、ack 作废）
- **导入与导出**：nvchecker keyfile（TOML）100% 无损导入（pacman/alpm 透明转官方 API）；JSON 导出（默认脱敏）
- **多用户与权限**：admin / user / viewer 三级角色、细粒度操作权限、资源归属隔离、注册开关与邮箱验证码、全局邮件配置
- **网站管理**：站点级 API 镜像、源站点默认值、系统告警渠道、OIDC Provider 与注册/邮件配置
- **帮助中心**：33 种源参考、13 种渠道指南、模板变量与预设、双轨对比与 nvchecker 迁移说明
- **中英双语**：界面与源/渠道元数据双语，语言偏好持久化，Vuetify 内置文案同步切换
- **运维与部署**：`/metrics` Prometheus 指标、单二进制 Dockerfile（非 root 运行）、systemd unit

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

## CI/CD 与质量门禁

CI 基于 Gitea Actions（`.gitea/workflows/`）：

- **check.yml** — push / PR 时自动执行：
  - Go 质量门禁（`scripts/check-go-quality.sh`）：gofmt、禁止生产代码使用 `interface{}`、`go vet`、`staticcheck`（带 `.lint-baseline/` 基线棘轮，只拦截新增告警）、`deadcode -test`（测试引用的辅助方法不算死代码）
  - `go test -race` 带覆盖率
  - `govulncheck` 依赖漏洞扫描
  - 编译验证（质量 job 不构建前端，放一个占位 `web/dist` 供 `go:embed` 解析，走真实嵌入路径；正式产物由 release.yml 构建）
  - 前端 `pnpm install --frozen-lockfile` + `pnpm test` + `pnpm build`（pnpm 版本由 `web/package.json` 的 `packageManager` 固定）
  - PR 提交信息校验（`type: subject` / `type(scope): subject` 规范，见 `scripts/check-commit-messages.sh`）
- **release.yml** — 打 `v*` tag 时构建 Linux amd64 / arm64 二进制与 Arch Linux 包并创建 Release；版本号经 `-X main.release=<tag>` 注入（`relaywatch -version` 可查）

内存受限的 runner 上，deadcode 与 `-race` 测试可能被内核 OOM 杀掉（日志 `signal: killed`）：两者已用 GOGC/GOMEMLIMIT 压低峰值（实测 deadcode 约 1.2GB、race 测试约 1.0GB）；仍被杀时可给 check.yml 对应步骤设 `RW_SKIP_DEADCODE: "1"` 跳过 deadcode（建议优先把 runner 内存加到 2GB+）。

本地开发可运行与 CI 相同的门禁：

```bash
make lint        # gofmt / interface{} / go vet / staticcheck / deadcode
make check-vuln  # govulncheck 漏洞扫描
make quality     # lint + 竞态测试 + 漏洞扫描
```

staticcheck / deadcode / govulncheck 通过 go.mod 的 `tool` 指令声明，`go tool <name>` 自动下载对应版本，无需手动安装。Arch 包也可本地构建：`cd packaging/arch && makepkg -si`。

## 技术栈

**后端**：Go 1.27+ / chi v5 / modernc.org/sqlite（纯 Go，无 cgo）

**前端**：Vue 3.5 / Vuetify 4.3 / Vite 8 / TypeScript 5.9 / Pinia 4.1 / vue-router 5.4

**部署**：`make build` 先构建前端到 `web/dist`，再由 `go:embed` 打进二进制。用户拿到单个可执行文件，运行时零依赖。

详见[设计方案](docs/design.md)。
