<p align="center">
  <img src="web/src/assets/logo.svg" width="96" alt="RelayWatch 品牌标「接力彗星」">
</p>

<h1 align="center">RelayWatch</h1>

<p align="center">
  自托管的版本更新监控服务：网页配置监控上游、自动检测新版本、多渠道推送通知。
</p>

<p align="center">
  <a href="https://github.com/xmengnet/relaywatch/actions/workflows/check.yml"><img src="https://github.com/xmengnet/relaywatch/actions/workflows/check.yml/badge.svg" alt="Check"></a>
  <a href="https://github.com/xmengnet/relaywatch/releases"><img src="https://img.shields.io/github/v/release/xmengnet/relaywatch?display_name=tag&label=release" alt="Release"></a>
</p>

## 为什么是 RelayWatch

- **nvchecker** 版本检测精确，但纯 CLI、配置手写 TOML、通知只有桌面弹窗
- **changedetection.io** 有 Web UI 和丰富推送，但只做网页文本 diff，不懂版本号语义，误报多
- **GitHub Watch** 只支持 GitHub，只发邮件

RelayWatch 把版本检测的精确性和 Web 操作的体验结合起来：网页上配监控，后端按语义比较版本，新版本、落后、追平都有明确事件，通知直接推到你的 IM。

## 核心能力

| 能力 | 说明 |
|---|---|
| 全生态多源监控（33） | GitHub / GitLab / Gitea / Bitbucket / Pagure / 通用 Git (ls-remote) / AUR / Arch 官方 / Debian / Ubuntu / PyPI / npm / crates / Go / RubyGems / Maven / Packagist / CPAN / CRAN / Hackage / OCI 容器 (GHCR/Quay/Harbor) / VS Marketplace / Open VSX / Snapcraft / HTTP 正则 / HTTP JSON / HTTP Header 重定向 / 手动基线 / 受控本地命令 |
| 双轨对比（AUR vs 上游） | 将 AUR/本地打包指向对应上游参照，检测包落后与追平；智能对齐并剥离 epoch 与 pkgrel，防止误报 |
| 实时事件推送 | SSE 实时通道（`/api/v1/events`），长任务与版本检查完成无刷新即刻响应 |
| 高级版本重写与过滤 | semver 比较 + 非标分段比较，支持前缀后缀剥离、include/ignore 过滤与 `from_pattern`/`to_pattern` 正则重写流水线 |
| 多渠道通知（13） | Telegram / Discord / Slack / ntfy / Gotify / SMTP / 飞书 / 企业微信 / 钉钉 / PushDeer / Server酱 / Bark / 通用 Webhook |
| nvchecker 无损导入 | 100% 兼容 nvchecker keyfile（TOML）批量导入，pacman/alpm 透明代换为官方 REST API，JSON 导出备份 |
| 企业级认证与安全 | 本地账户（bcrypt）+ 多 OIDC Provider（自定义图标、Claim 限制）+ API Token，敏感字段 AES-256-GCM 加密 |
| 多用户与权限 | admin / user / viewer 三级角色、细粒度操作权限、资源归属隔离、注册开关与邮箱验证码 |
| Web 界面 | Vue 3 + Vuetify 4.3 SPA，中英双语，单二进制 `go:embed` 嵌入，零外部依赖运行 |

## 监控源一览（33）

- **代码托管与 VCS**：`github_release` / `github_tag` / `gitlab_release` / `gitea_release` / `bitbucket` / `pagure` / `git_tag` (通用 Git)
- **编程语言包生态**：`pypi` / `npm` / `crates` / `go_module` / `rubygems` / `maven` / `packagist` / `cpan` / `cran` / `hackage`
- **版本追踪与上游聚合**：`repology` / `anitya`
- **Linux 发行版**：`aur` / `arch_official` / `debianpkg` / `ubuntupkg` (nvchecker `pacman`/`alpm` 自动映射代换)
- **容器镜像**：`container` (标准 OCI V2：GHCR / Quay / Harbor) / `docker_tag` (Docker Hub)
- **通用网络提取**：`http_regex` / `http_json` / `http_header` (重定向/HEAD)
- **生态市场与系统源**：`openvsx` / `vsmarketplace` / `snapcraft` / `manual` / `cmd` (受控沙箱)

每种源的参数说明见应用内「帮助中心」，或 [设计方案](docs/design.md) 对应章节。

## 快速开始

从 [Releases](https://github.com/xmengnet/relaywatch/releases) 下载对应架构的二进制（前端已内嵌，运行时零依赖）：

```bash
cp config.example.yaml config.yaml                          # 按需调整监听地址等
export RELAYWATCH_SECRET_KEY="$(openssl rand -base64 32)"   # 必须设置：丢失则已加密的 token/webhook 配置无法恢复，请备份
./relaywatch-linux-amd64 -config config.yaml
# 浏览器打开 http://localhost:8080，首次访问引导创建管理员
```

Arch Linux 用户可从 Releases 下载 `.pkg.tar.zst` 后 `sudo pacman -U` 安装（配置位于 `/etc/relaywatch/`，密钥写入 `/etc/relaywatch/env`）。

Docker、systemd、反向代理、备份与密钥轮换等完整部署说明见 [docs/deploy.md](docs/deploy.md)。

## 文档

- [需求文档](docs/requirements.md) — 功能范围、验收标准
- [设计方案](docs/design.md) — 技术选型、数据模型、接口、调度、认证、安全
- [部署文档](docs/deploy.md) — Docker、systemd、反向代理、备份

## 构建与开发

```bash
make build        # pnpm build + go build，产出 ./relaywatch
```

**必须配置加密密钥**（未设置时拒绝启动）：`openssl rand -base64 32` 写入配置 `security.secret_key` 或环境变量 `RELAYWATCH_SECRET_KEY`；轮换用 `security.old_secret_key` 过渡。

```bash
# 前端热更新开发
go run ./cmd/relaywatch              # 终端 1（:8080）
cd web && pnpm dev                   # 终端 2（:5173，API 代理到 8080）
```

环境要求：Go 1.27+、Node 22+、pnpm。前端固定使用 TypeScript 5.9（vue-tsc 3.3.13 与 TypeScript 7 不兼容），`package.json` 已锁定。

本地可运行与 CI 相同的门禁：

```bash
make lint        # gofmt / interface{} / go vet / staticcheck / deadcode
make check-vuln  # govulncheck 漏洞扫描
make quality     # lint + 竞态测试 + 漏洞扫描
```

staticcheck / deadcode / govulncheck 通过 go.mod 的 `tool` 指令声明，`go tool <name>` 自动下载对应版本，无需手动安装。Arch 包也可本地构建：`cd packaging/arch && makepkg -si`。

## CI/CD

GitHub Actions（`.github/workflows/`）：

- **check.yml** — push / PR 时执行：Go 质量门禁（gofmt、`interface{}` 检查、`go vet`、staticcheck 基线棘轮、deadcode）、`go test -race` 带覆盖率、`govulncheck` 依赖漏洞扫描、真实 `go:embed` 路径编译验证、前端 `pnpm test` + `pnpm build`；PR 另有提交信息规范校验
- **release.yml** — 推送 `v*` tag 时构建 Linux amd64 / arm64 二进制与 Arch Linux 包并自动创建 GitHub Release；版本号经 `-X main.release=<tag>` 注入（`relaywatch -version` 可查）

## 技术栈

**后端**：Go 1.27+ / chi v5 / modernc.org/sqlite（纯 Go，无 cgo）

**前端**：Vue 3.5 / Vuetify 4.3 / Vite 8 / TypeScript 5.9 / Pinia 4.1 / vue-router 5.4

**部署**：`make build` 先构建前端到 `web/dist`，再由 `go:embed` 打进二进制。用户拿到单个可执行文件，运行时零依赖。
