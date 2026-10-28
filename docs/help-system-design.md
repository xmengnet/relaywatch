# RelayWatch 帮助中心与渠道模板交互设计方案

本文档记录了 RelayWatch「帮助中心」及「通知渠道模板助手」的设计想法与落地规范，供后续功能开发与前端实现参考。

---

## 1. 背景与目标

### 1.1 现状与痛点
RelayWatch 经过迭代，已经具备非常强大的版本监控与告警能力：
- 支持多达 **33 种监控源**（覆盖代码托管、Linux 发行版、各种编程语言包生态、容器镜像、应用市场以及通用 HTTP/受控 Shell 命令）；
- 支持 **13 种通知推送渠道**（钉钉、企业微信、飞书、Telegram、Discord、Slack、ntfy、Gotify、Bark、PushDeer、Server酱、邮件 SMTP、通用 Webhook）；
- 支持多级版本清洗管道（前缀后缀剥离、正则表达式包含/排除、正则分组替换重写）与独特的**双轨对比工作流**（落后/追平告警）。

但在实际使用中存在显著的**学习曲线问题**：
1. **源配置不直观**：用户往往不清楚某类软件应该选用哪种源，各源所需要的字段格式（如 `owner/repo`、点路径 `data.version`、OCI 规范镜像格式等）容易填错；
2. **模板变量门槛高**：自定义模板基于 Go `text/template`，用户不知道有哪些内置变量可用（如 `.Monitor.Name`、`.Change.NewVersion`、`.Compare.State`）、如何写条件分支或格式化时间；
3. **缺少系统性指引**：没有集中式文档供用户查阅，nvchecker 的老用户不知道哪些 TOML 字段能自动转换。

### 1.2 目标定位
构建**「全局帮助手册 + 就地模板助手 + 场景化预设」三位一体**的帮助体验：
- **全局帮助中心（`/help` 路由）**：结构化的参考手册，覆盖快速上手、33 种源参考、13 种渠道指南、模板语法、双轨对比与 nvchecker 迁移；
- **渠道就地模板助手（`ChannelListView.vue`）**：在编辑渠道模板时，直接提供变量芯片插入、精选模板一键套用和实时提示；
- **场景化预设与跳转**：各功能页面（监控编辑、批量导入）可随处直达对应章节或一键填入演示模板。

---

## 2. 全局帮助中心规划（`/help` 路由）

### 2.1 路由与导航入口
- **路由路径**：`/help`
- **菜单入口**：
  - 左侧主导航栏：增加「帮助中心」（图标 `mdi-help-circle-outline`，位于通知与个人资料之间）；
  - 顶部 AppBar：在主题切换按钮旁放置 `?` 帮助图标按钮，点击随时跳转；
- **URL 参数联动**：支持 `/help?tab=channels`、`/help?tab=sources` 等 query 参数，方便外部页面（如渠道弹窗）精确深链跳转。

### 2.2 核心内容模块划分

#### 模块一：⚡ 快速入门（Quickstart）
- **3 步开启追踪**：
  1. 创建通知渠道并发送测试验证；
  2. 新建监控项并绑定渠道；
  3. 观察自动探测、版本变更与手动检查（Check Now）。
- **核心概念解析**：
  - **当前基准版本（Current Version）** vs **最新探测版本（Latest Version）**；
  - **采纳更新（Bump）** 与 **回滚（Rollback）** 的版本生命周期；
  - **通知静默期（Cooldown）**：如何利用冷静期防止上游微小发版产生的通知风暴；
  - **监控状态机**：`up_to_date`（最新）、`has_update`（有更新）、`error`（网络/解析错误）、`missing`（上游明确 404 下架）。

#### 模块二：📦 33 种监控源配置手册（Sources Reference）
提供分类过滤（全部、代码托管、发行版与包管理、编程语言生态、容器与扩展市场、通用网络与定制）与实时搜索：

| 分类 | 监控源类型 | 核心字段与说明 | 典型示例 |
| :--- | :--- | :--- | :--- |
| **代码托管** | `github_release` | `repo` (owner/name), `include_prerelease`, `token` | `{"repo": "torvalds/linux"}` |
| | `github_tag` | `repo`, `tag_pattern` (正则过滤最大 tag), `token` | `{"repo": "vuejs/core", "tag_pattern": "^v[0-9.]+$"}` |
| | `gitlab_release` | `host` (域名), `project` (group/repo 或 ID), `token` | `{"host": "https://gitlab.com", "project": "gitlab-org/gitlab"}` |
| | `gitea_release` | `host`, `repo`, `token` | `{"host": "https://gitea.com", "repo": "owner/repo"}` |
| | `git_tag` | `url` (https/git 协议), `branch`, `use_commit`, `tag_pattern` | `{"url": "https://git.kernel.org/.../linux.git"}` |
| | `bitbucket` | `repo` (workspace/repo), `branch`, `tag_pattern` | `{"repo": "atlassian/example"}` |
| | `pagure` | `repo`, `host` (默认 https://pagure.io) | `{"repo": "pagure"}` |
| **发行版包管** | `aur` | `pkgname` (Arch AUR 包名) | `{"pkgname": "yay"}` |
| | `arch_official` | `pkgname`, `repo` (可限定 core / extra 等) | `{"pkgname": "linux", "repo": "core"}` |
| | `debianpkg` | `pkgname`, `suite` (默认 sid), `strip_release` (去 -1 修订号) | `{"pkgname": "ripgrep", "suite": "sid", "strip_release": true}` |
| | `ubuntupkg` | `pkgname`, `suite` (例如 noble), `strip_release` | `{"pkgname": "curl", "suite": "noble"}` |
| | `repology` | `project`, `repo` (目标发行版), `subrepo` | `{"project": "ripgrep", "repo": "arch", "subrepo": "extra"}` |
| | `anitya` | `project` (release-monitoring.org 项目 ID) | `{"project": "13815"}` |
| **编程语言生态** | `go_module` | `module` (Go 模块完整路径，查 @latest) | `{"module": "github.com/gin-gonic/gin"}` |
| | `npm` | `package` (支持 @scope 包), `dist_tag` (默认 latest) | `{"package": "vue", "dist_tag": "latest"}` |
| | `pypi` | `project` (Python 包名) | `{"project": "requests"}` |
| | `crates` | `crate` (Rust Crate 名) | `{"crate": "tokio"}` |
| | `maven` | `group`, `artifact`, `repo` (默认 Maven Central) | `{"group": "org.apache.commons", "artifact": "commons-lang3"}` |
| | `packagist` | `package` (vendor/name), `include_prerelease` | `{"package": "monolog/monolog"}` |
| | `rubygems` | `gem`, `include_prerelease` | `{"gem": "rails"}` |
| | `cpan` | `module` (Perl 模块名) | `{"module": "Mojolicious"}` |
| | `cran` | `package` (R 语言包名) | `{"package": "ggplot2"}` |
| | `hackage` | `package` (Haskell 包名，查 preferred) | `{"package": "pandoc"}` |
| **容器与扩展** | `container` | `image` (含 registry 域名如 ghcr.io), `tag_regex` | `{"image": "ghcr.io/owner/repo", "tag_regex": "^v[0-9.]+$"}` |
| | `docker_tag` | `image` (自动加 library/), `tag_regex` | `{"image": "library/nginx"}` |
| | `openvsx` | `extension` (publisher.name) | `{"extension": "rust-lang.rust-analyzer"}` |
| | `vsmarketplace`| `extension` (publisher.name) | `{"extension": "golang.go"}` |
| | `snapcraft` | `snap`, `channel` (默认 stable) | `{"snap": "canonical-livepatch", "channel": "stable"}` |
| **通用与定制** | `http_json` | `url`, `path` (点路径如 data.version 或 items.0.tag), `headers` | `{"url": "https://api.example.com/ver", "path": "data.version"}` |
| | `http_regex` | `url`, `regex` (带捕获组), `group` (默认 1), `headers` | `{"url": "https://example.com/dl.html", "regex": "v([0-9.]+)"}` |
| | `http_header` | `url`, `header` (默认 Location), `regex`, `method` (HEAD/GET) | `{"url": "https://github.com/x/y/releases/latest", "header": "Location"}` |
| | `manual` | `version` (手动版本基线), `url` | `{"version": "1.0.0", "url": "https://example.com"}` |
| | `cmd` | `cmd` (受控 Shell 命令提取首行), `timeout` (默认 15s) | `{"cmd": "curl -s https://example.com/ver.txt"}` |

每种源提供**一键复制 JSON 示例**。

#### 模块三：🔔 渠道与通知模板（Channels & Templates）
- **13 种推送渠道配置速查**：
  说明各渠道申请 Webhook 或 Token 的入口（飞书、钉钉、企微、Telegram、Discord、Slack、Bark、ntfy、Gotify、PushDeer、Server酱、邮件、通用 Webhook）。
- **模板语法与内置变量速查表**：
  - `.Monitor.Name`：监控项目名称；
  - `.Monitor.SourceType`：监控源类型；
  - `.Monitor.Tags`：标签列表；
  - `.Change.NewVersion`：探测到的新版本号；
  - `.Change.OldVersion`：升级前的旧版本号；
  - `.Change.DetectedAt`：发现时间（可使用 `.Format "2006-01-02 15:04:05"`）；
  - `.URL`：上游版本链接；
  - `.Compare.RefMonitorName`、`.Compare.RefVersion`、`.Compare.FollowerVersion`、`.Compare.State`、`.Compare.Event`。
- **模板实战示例（支持一键复制代码）**：标准通知版、Markdown 结构化卡片版、极简单行手机推送版、双轨对比专用版。

#### 模块四：⚖️ 双轨版本对比指南（Dual-track Comparison）
- **设计初衷**：解决打包维护者「我的下游打包版本是否落后于官方源码版本」的痛点；
- **配置步骤**：
  1. 建立上游监控项（如跟踪 GitHub Release）；
  2. 建立从属监控项（如跟踪 Arch 官方源或 AUR），并在编辑页勾选「参考监控项」指向第一步的监控；
- **三大事件驱动机制**：
  - `entered_lagging`：原本同步，上游发新版进入落后；
  - `gap_grew`：上游连续发版，落后差距扩大；
  - `caught_up`：从属版本跟进，追平或反超上游。

#### 模块五：🔄 nvchecker 迁移手册
- **TOML 映射规则**：nvchecker 各 `source` 选项在 RelayWatch 中的自动对齐规则；
- **常见支持/不支持项**：说明全局 `__config__` 的过滤逻辑与 `cmd` 本地脚本受控安全策略；
- **典型样例与一键跳转**：提供样例 TOML 片段与跳转到 `/import` 批量导入页面的按钮。

#### 模块六：🎯 版本提取与高级改写（Filters）
- **前缀/后缀剥离**（`version_prefix` / `version_suffix`）：如从 `v1.2.3` 提取 `1.2.3`；
- **正则白名单/黑名单**（`include_regex` / `exclude_regex`）：过滤预发布版本；
- **正则替换重写**（`from_pattern` / `to_pattern`）：例如将年月日格式 `20261028` 重写为 `2026.10.28` 参与语义化比较。

---

## 3. 渠道编辑「模板助手与预设实例」交互设计

### 3.1 界面位置与组件设计（`ChannelListView.vue`）
在渠道新建/编辑对话框中的 `v-textarea`（自定义模板输入框）正下方，嵌入一个展开式「模板助手」区域：

```
┌─────────────────────────────────────────────────────────────┐
│ 自定义模板（可选）                                           │
│ [ {{ .Monitor.Name }} 发现新版本...                       ] │
│ 基于 Go text/template 渲染；留空使用默认模板                │
├─────────────────────────────────────────────────────────────┤
│ [📖 展开模板写法与实用实例]        [查看完整文档 (去帮助中心) ↗] │
├─────────────────────────────────────────────────────────────┤
│ 💡 变量快捷插入（点击芯片插入至光标或末尾）：                 │
│   监控信息:  [ {{ .Monitor.Name }} ]  [ {{ .Monitor.Tags }} ]│
│   版本信息:  [ {{ .Change.NewVersion }} ] [ {{ .URL }} ] ... │
│   对比信息:  [ {{ .Compare.RefVersion }} ] ...              │
├─────────────────────────────────────────────────────────────┤
│ 📋 精选模板预设（点击直接套用）：                            │
│   [ 通用标准卡片 ]  [ 一键套用 ]                             │
│   [ 极简单行 (适合短信/Bark) ]  [ 一键套用 ]                  │
│   [ Markdown 丰富卡片 (飞书/钉钉/企微) ]  [ 一键套用 ]       │
│   [ 双轨对比专用模板 ]  [ 一键套用 ]                         │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 交互细节要求
1. **防破坏已有内容**：用户点击「套用模板」时，若文本框已有内容，可给予确认提示或直接覆盖并弹出 Snackbar「已套用预设模板」；
2. **变量插入**：点击变量标签后自动追加并在光标处补齐空格，并提供 Tooltip 显示该变量的中文含义；
3. **响应式友好**：预设卡片在移动端自适应折叠，字体使用等宽字体（`font-mono`）。

---

## 4. 后续落地实施路线

1. **第一阶段：实现渠道模板助手（`ChannelListView.vue`）**
   - 提取预设模板常量与变量字段定义；
   - 调试 UI 交互，确保输入体验流畅；
2. **第二阶段：实现 `/help` 路由与 `HelpView.vue` 核心组件**
   - 编写优雅的多 Tab 页面，将所有 33 种源和模板示例结构化展示；
   - 模板代码与 Go text/template 文本统一在 `<script setup>` 声明常量，规避 Vue 模板编译器插值冲突；
   - 在主布局和侧边栏注册导航项；
3. **第三阶段：场景化增强**
   - 在监控创建页增加「源配置说明抽屉」；
   - 在批量导入页增加「载入 nvchecker 示例」按钮。
