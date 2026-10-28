# RelayWatch 需求文档

版本：0.3.1（草案）
日期：2026-10-27

变更：v0.1 → v0.2 根据评审意见修订，新增导入/导出、API Token、版本过滤、批量确认、系统告警渠道；状态机增加 `missing` 态；明确静默期与版本回退语义；调度增加抖动与 per-host 限速。

v0.2 → v0.2.1 补充静默期与确认/忽略的交互规则：`deferred` 投递在用户 ack、手动重发、监控删除或停用时立即作废，作废为终态；静默期结束的补发需重新校验监控仍处于 `has_update`。

v0.2.1 → 实现状态（2026-10-27）：M1–M4 已实现并冒烟验收。验收标准 2/3/4(webhook 替代)/5/9/10/11/12 已实测通过；标准 1（引导流程）通过；标准 6（OIDC）代码就绪、待真实 IdP 实测；标准 7（重启恢复）由调度基于 last_checked_at 的设计保证、待专项测试。手动重发补充语义：对已 ack 的记录允许 Force 重发（跳过周期校验，版本取发送时刻 latest）。

v0.2.1 → v0.3.0（M6 规划增补，2026-10-27）：新增 §3.8 版本对比（双轨）与验收标准 13–18；§5 中「上游 vs 本地双轨对比」由 v2 候选转为已规划；里程碑增加 M6。注意：§3.8 为规划，尚未实现（M1–M5 为已实现范围）。

v0.3.0 → v0.3.1（nvchecker 全面兼容与扩展源规划，2026-10-27）：目标全面对齐 nvchecker 的 44 种监控源，保证现有 nvchecker keyfile TOML 导入 100% 可用；在 §3.1 增加扩展源分类规划与规范化高级规则（from_pattern/to_pattern）；里程碑增加 M7。

实现状态（2026-10-27 续）：M6 规划全部落地——§3.8 双轨对比与 §3.4 SSE 实时推送全栈实现（数据模型、迁移 0005、版本对齐层、评估器、Checker 钩子、通知管线、EventBroker、前端 useSse 与无刷新更新全部完成并测试通过）；§3.1 中的 33 种监控源与 from_pattern/to_pattern 正则重写流水线全部落地并无损导入通过。

## 1. 项目背景

维护 AUR 包、自建软件或跟踪第三方项目时，需要知道上游是否发布了新版本。人工盯 GitHub Release 页有延迟，现有工具又各有短板：

| 现有方案 | 短板 |
|---|---|
| GitHub 原生 Watch | 只支持 GitHub，只发邮件，不能跟踪 AUR 等其它源 |
| nvchecker | 纯 CLI，配置只能手写 TOML，没有 Web 界面，内置通知仅桌面弹窗 |
| changedetection.io | 网页文本 diff，不懂版本号语义（`0.1.9` 与 `0.1.10` 比不出大小），误报多，依赖重 |
| aur-out-of-date | 只判断 AUR 是否落后，不跟踪上游中间版本 |

RelayWatch 的目标：**把 nvchecker 式的精确版本检测，和 changedetection.io 式的 Web 操作体验结合起来**，并支持多种认证方式和可插拔的推送渠道。

## 2. 目标用户

- AUR 包维护者：同时跟踪多个包的上游版本
- 自建服务运维：跟踪上游依赖的新版本
- 个人开发者：跟踪几十个开源项目的 release

## 3. 功能需求

### 3.1 监控源（Source）

每条监控记录跟踪一个"上游"，必须支持以下源类型。源类型可扩展。

| 源类型 | 说明 | 必需参数 | 可选参数 |
|---|---|---|---|
| `github_release` | GitHub 最新 Release | `repo`（`owner/name`） | `include_prerelease`、`token`、`use_release_name` |
| `github_tag` | GitHub 最大版本 tag | `repo` | `tag_pattern`、`token` |
| `gitlab_release` | GitLab 最新 Release | `host`、`project` | `token` |
| `gitea_release` | Gitea / Forgejo Release | `host`、`repo` | `token` |
| `aur` | AUR 包当前版本 | `pkgname` | — |
| `arch_official` | Arch 官方仓库版本 | `pkgname` | `repo`（可选；缺省查 core/extra/multilib 全部官方仓库，可指定如 `extra` 或 `extra-testing`） |
| `pypi` | PyPI 项目最新版本 | `project` | `include_prerelease` |
| `npm` | npm 包最新版本 | `package` | `dist_tag` |
| `crates` | crates.io 版本 | `crate` | — |
| `go_module` | Go module 版本 | `module` | — |
| `http_json` | 任意 JSON 接口 | `url`、`path`（gjson 路径语法，如 `data.version`） | `headers` |
| `http_regex` | 任意网页正则抓取 | `url`、`regex` | `headers`、`group` |
| `docker_tag` | 容器镜像 tag | `image` | `tag_regex` |

**nvchecker 兼容扩展规划（全面对齐 44 种 nvchecker source）**：

为确保导入 nvchecker 的 TOML 配置 100% 顺畅并覆盖各类技术栈，新增规划以下监控源：

| 规划分类 | 包含源类型与说明 | 对标 nvchecker 源 |
|---|---|---|
| **上游版本聚合服务** | `repology` (Repology API 跨发行版聚合)、`anitya` (release-monitoring.org 开源上游追踪) | `repology`、`anitya` |
| **语言包管理器** | `rubygems` (RubyGems)、`maven` (Maven Central / 自建仓库 XML 元数据)、`packagist` (PHP Composer)、`cpan` (Perl)、`cran` (R)、`hackage` (Haskell)、`opam` (OCaml) | `gems`、`maven`、`packagist`、`cpan`、`cran`、`hackage`、`opam` |
| **通用 VCS 与代码托管** | `git_tag` (通用 Git 远程 tag，免平台 API 限制)、`bitbucket` (Bitbucket Tags/Releases)、`pagure` (Pagure Git Tags) | `git`、`git_dulwich`、`git_pygit2`、`bitbucket`、`pagure` |
| **通用网络提取扩展** | `http_header` (HEAD/GET 抓取响应头如 Location 重定向)、`htmlparser` (XPath / 节点解析) | `httpheader`、`htmlparser` |
| **容器镜像扩展** | `container` (遵循标准 OCI Registry V2 规范，支持 GHCR、Quay、私有仓库) | `container` |
| **Linux 发行版生态** | `debianpkg` (Debian sources API)、`ubuntupkg` (Launchpad API)、`apt` (远程 InRelease/Packages)、`rpmrepo` (远程 repomd.xml) | `debianpkg`、`ubuntupkg`、`apt`、`rpmrepo` |
| **生态应用市场** | `vsmarketplace` (VS Code 插件)、`openvsx` (Open VSX)、`snapcraft` (Canonical Snap) | `vsmarketplace`、`openvsx`、`snapcraft` |
| **系统与受控源** | `manual` (静态手动版本)、`cmd` (严格受控的本地脚本执行，默认禁止) | `manual`、`cmd` |

*注：针对 nvchecker 的 `pacman`/`alpm` 本地源，因 RelayWatch 为容器化/跨平台 Web 服务，导入时自动转换为 `arch_official` 远程 API 抓取，免除本地 pacman 数据库依赖。*

**版本过滤规则**（每条监控可选）：
- `version_include_regex`：只有匹配的版本才参与更新判断（如 `^1\.` 只跟踪 1.x）
- `version_ignore_regex`：匹配的版本被跳过（如 `nightly|snapshot`）
- `version_ignored`：空格分隔的显式跳过版本列表（对标 nvchecker `ignored`）
- 过滤在版本规范化之后、比较之前执行；被过滤的版本仍记入检查历史，但不改变状态、不触发通知

**版本号提取与规范化高级规则**：
- 可配置 `prefix` / `suffix` 剥离（如 `v` 前缀）
- 正则重写：`from_pattern` 与 `to_pattern`（对标 nvchecker，支持将如 `(\d{4})(\d{2})(\d{2})` 转换为 `\1.\2.\3`）
- 版本比较使用语义化版本（semver），无法解析时退化为分段比较/字符串比较并给出警告
- 源返回原始版本字符串，入库前做规范化

**每条监控记录的字段**：
- 名称（用户可读）
- 源类型及参数
- 版本前缀/后缀、正则重写规则（`from_pattern` / `to_pattern`）
- 检查间隔（可继承全局默认）
- 启用/停用开关
- 通知渠道绑定（多选）
- 静默期（同一版本重复通知的最小间隔）
- 备注、标签

### 3.2 版本检测与状态机

每条监控记录有明确状态：

```
          首次检查
   ┌──────────┐
   │ unknown  │  ← 尚无基线版本
   └────┬─────┘
        │ 首次成功
        ▼
   ┌──────────┐   检测到更高版本   ┌────────────┐
   │  up_to_date  ─────────────────▶│ has_update │
   └──────────┘   ◀─────────────────└────────────┘
        ▲  ▲           确认/忽略后
        │  │
        │  │ 上游明确不存在（404 / not found，可恢复）
        │  └──────────────────┐
        │                     ▼
        │ 检查失败      ┌──────────┐
        ▼               │ missing  │
   ┌──────────┐         └──────────┘
   │  error   │     missing 表示上游已消失（仓库删除、
   └──────────┘     包下架），提示用户该监控该清理了；
                   与暂时性故障 error 区分开
```

**状态转换细则**：
- `missing`：仅当源明确返回「不存在」（HTTP 404、AUR 包查无此项等）时进入；恢复存在后自动回到 `up_to_date` 并保留基线
- `error`：网络超时、5xx、解析失败等暂时性故障；连续失败达阈值后进入，恢复成功后自动回 `up_to_date` 或 `has_update`
- `has_update` 期间检测到**更高**的新版本：只更新 `latest_version` 和既有未确认的变更记录，**不重复投递通知**（与静默期联动，见 3.3）
- 版本回退（上游删了 release）：记入版本变更历史，`latest_version` 跟随上游，**`current_version` 基线不动**，状态保持原值，不通知；上游再次超过基线时正常触发更新流程

**必须记录的历史**：
- 每次检查的时间、耗时、结果、错误信息
- 版本变更历史（从哪个版本到哪个版本、发现时间、是否已通知）
- 失败时保留最后一次成功版本，不清空

**关键行为**：
- 首次检查只建立基线，**不触发通知**（避免接管旧数据时刷屏）
- 连续失败达到阈值后，向**系统告警渠道**（全局配置的一组渠道，与监控绑定的通知渠道解耦）发送"源不可用"告警
- 进入 `missing` 状态同样走系统告警渠道

### 3.3 通知渠道（Notifier）

每个渠道是一条可复用的配置，可被多条监控记录引用。

| 渠道类型 | 说明 |
|---|---|
| `webhook` | 通用 HTTP POST，自定义 URL、headers、body 模板 |
| `telegram` | Bot Token + chat_id |
| `discord` | Webhook URL |
| `slack` | Incoming Webhook URL |
| `ntfy` | 服务地址 + topic + 可选 token |
| `gotify` | 服务地址 + token |
| `email_smtp` | SMTP 服务器、端口、账号、密码、发件人；收件人可选，留空默认发送到渠道所有者的账号邮箱 |
| `feishu` | 飞书机器人 Webhook |
| `wecom` | 企业微信群机器人 Webhook |
| `dingtalk` | 钉钉机器人 Webhook |
| `pushdeer` / `serverchan` / `bark` | 国内常见推送服务 |

**消息模板**：
- 内置默认模板，包含监控名称、旧版本、新版本、上游链接、发现时间
- 支持自定义 Go `text/template` 模板，可访问结构化字段
- 支持 Markdown / 纯文本两种模式（不同渠道能力不同）

**投递语义**：
- 每个渠道独立记录投递结果（成功/失败/重试次数）
- 失败按指数退避重试，最多 N 次（可配置）
- 每个版本变更周期（一次未确认的 has_update）对同一渠道只投递一次，除非手动重发
- 支持"测试"按钮，立即发送一条测试消息

**静默期（`notify_cooldown_sec`）语义**：
- 作用于「同一变更周期内连续出现多个新版本」的合并策略
- 检测到新版本时若距上次投递不足静默期：只更新 `latest_version` 与变更记录，跳过本次投递；静默期结束后若仍处于 has_update，补发一条（内容为当时的最新版本）
- 为 0 时每个新变更周期都投递，但周期内仍只投一次
- 系统告警（源不可用、missing）不受静默期限制，但同一故障事件只告警一次

**静默期与确认/忽略的交互**（`deferred` 投递的作废规则）：
- 处于静默期时产生的待投递记录（`deliveries.status = 'deferred'`）在以下情况**立即作废**（置 `status = 'canceled'`，不再补发）：
  - 用户手动确认（ack）或忽略该更新，监控回到 `up_to_date`
  - 用户手动重发同一变更
  - 监控被删除或停用
- 作废是终态，不参与重试；通知历史中显示为「已取消」并注明原因
- 静默期结束时，worker 只在监控仍为 `has_update` **且**该 `deferred` 记录仍属于当前未确认的变更周期时才补发；否则按上一条作废
- 补发内容的版本号取**补发时刻**的 `latest_version`，而非记录创建时的版本

### 3.4 Web 界面

**核心页面**：

1. **仪表盘**
   - 有更新的监控数量、异常数量、总监控数
   - 最近通知记录
   - 最近失败的检查

2. **监控列表**
   - 表格：名称、源类型、当前版本、最新版本、状态、上次检查、操作
   - 筛选：按状态、标签、源类型
   - 搜索：按名称、仓库名
   - 排序：支持按「最近变更时间」排序（有更新的排前面）
   - 批量操作：启用/停用、立即检查、**确认更新（ack）**、删除

3. **监控详情/编辑**
   - 表单配置所有字段，源类型切换时动态显示对应参数
   - 「立即检查」按钮，实时反馈结果
   - 版本历史表格
   - 检查历史（含错误详情）

4. **通知渠道管理**
   - 渠道列表、增删改
   - 每个渠道的「发送测试」按钮
   - 投递记录查看

5. **系统设置**
   - 全局检查间隔
   - 并发上限
   - 系统告警渠道（选择若干已配置渠道接收源不可用 / missing 告警）
   - 认证配置（见 3.5）
   - 日志级别

6. **通知历史**
   - 全部投递记录，可按监控、渠道、结果筛选

7. **导入 / 导出**
   - 批量导入：粘贴 TOML/JSON 文本或上传文件，格式兼容 nvchecker 的 keyfile 配置（源字段能映射的直接转换，nvchecker 专有选项忽略并列出警告），导入前预览解析结果
   - 导出：全部监控与渠道配置导出为 JSON，敏感字段可选脱敏（默认脱敏），用于备份与迁移

**交互要求**：
- 表单校验在前后端都做
- 长任务（立即检查、发送测试）不阻塞页面，用 SSE 或轮询反馈进度
- 所有破坏性操作二次确认
- 响应式，手机上也能操作

### 3.5 认证与授权

#### 3.5.1 本地账户

- 用户名 + 密码
- 密码使用 bcrypt（cost ≥ 12）或 argon2id 存储
- 首次启动引导创建管理员账户
- 支持修改密码
- 可选：TOTP 双因素（预留）

#### 3.5.2 OIDC

- 可配置多个 OIDC Provider（Issuer、Client ID、Client Secret、Scopes）
- 标准 Authorization Code Flow + PKCE
- 支持自动发现（`/.well-known/openid-configuration`）
- 从 ID Token 提取用户名、邮箱、头像
- 支持按 Claim 值限制登录（如 `groups` 包含某值，或 `email` 域名白名单）
- 首次 OIDC 登录自动创建本地用户记录（不存密码）

#### 3.5.3 会话管理

- Cookie 或 Bearer Token（API 用）双模式
- Cookie 设置 `HttpOnly`、`SameSite=Lax`、`Secure`（HTTPS 时）
- 会话可撤销（登出、管理员踢出）
- 会话过期时间可配置

**API Token**：
- 每个用户可创建多个 Token（名称、过期时间可选），用于脚本/CI 调用 `/api/v1`
- 只存哈希，创建时明文只显示一次
- 可随时吊销；记录最后使用时间

#### 3.5.4 权限模型

最小化设计，两种角色：
- `admin`：全部权限，管理用户和系统设置
- `viewer`：只读，可查看监控和通知记录，不能修改

若需要多用户隔离（每个用户只看自己的监控），列为 v2 需求。

#### 3.5.5 安全要求

- 所有写操作校验 CSRF Token
- 登录接口限流（防暴力破解）
- 敏感字段（token、密码、webhook URL）在 UI 中默认掩码显示
- 数据库中敏感字段加密存储（AES-GCM，密钥来自环境变量或配置文件）
- 审计日志记录：登录、配置变更、手动触发

### 3.6 调度

- 内置调度器，按每条记录的间隔执行
- 到期时间加入随机抖动（间隔的 ±10%），避免大量同间隔监控形成周期性请求脉冲
- **per-host 限速**：除全局并发上限外，同一主机名（如 `api.github.com`）有独立的每分钟请求数上限，可配置；GitHub 等限流严格的源不会因并发高而触发 429
- 尊重上游 `Retry-After` 响应头，收到 429 时按指示退避
- 支持单次手动触发（同步返回结果，不走调度队列）
- 进程重启后恢复调度计划，不丢失状态
- 同一监控记录不允许并发执行（加锁）

### 3.7 可观测性

- 结构化日志（JSON 可选）
- 健康检查端点 `/healthz`
- Prometheus 指标端点 `/metrics`（可选）：检查次数、失败次数、通知投递数、耗时直方图
- 可选：内置简单的运行状态页

### 3.8 版本对比（双轨对比）

> 状态：M6 规划，尚未实现。设计方案见 [design.md](./design.md) §17。

把同一软件的两条监控关联起来——一条上游发布源（如 GitHub Release），一条本地/发行版源（如 AUR）——回答「我的包落后了吗」，并在**落后出现、差距扩大、追平**时提醒。

**关联模型**：

- 监控记录增加可选的「上游参照」（`reference_monitor_id`），指向另一条监控；引用方称「跟随监控」，被引用方称「参照监控」
- 一条监控最多有一个参照；一个参照可被多条监控引用（如 `foo`、`foo-bin`、`foo-git` 同源）
- 参照监控自身不得再有参照（只允许一层，杜绝链式与环）
- 删除参照监控：引用方自动解除关联、对比状态清空、待投递的对比通知作废，自身作为普通监控继续工作

**对比状态**（存于跟随监控，与自身状态机正交）：

| 值 | 含义 |
|---|---|
| （未配置） | 无参照 |
| `in_sync` | 跟随方版本 = 参照方版本 |
| `lagging` | 跟随方版本 < 参照方版本 |
| `ahead` | 跟随方版本 > 参照方版本（如 AUR 包打了 patch），只展示不提醒 |

**版本对齐**（对比前按源类型执行）：

- `aur` 侧剥离尾部 pkgrel（`-<数字>`）与前导 epoch（`N:`），其余源类型原样；两侧各自应用
- 必要性：`0.1.19-1` 是合法 semver（`-1` 被解析为预发布），不剥离则 `0.1.19-1 < 0.1.19` 恒成立，AUR 场景会永远误报落后；对齐后 `0.1.19-1` ≡ `0.1.19`
- 对齐只作用于对比路径，不改变入库版本与「当前/最新版本」的展示

**通知事件**（发送到跟随监控绑定的渠道；复用投递队列的退避重试、静默期、历史与手动重发）：

| 事件 | 触发条件 |
|---|---|
| 进入落后 | 已知状态 → `lagging`（含刚配置好参照时发现已落后） |
| 差距扩大 | 保持 `lagging` 且参照方版本前进（按「上次通知时的参照版本」去重） |
| 已追平 | `lagging` → `in_sync` |

- 任一侧处于 `error`/`missing`/无版本时**跳过评估、保持上次对比状态**，避免故障期间误报「追平」
- 进入 `ahead`、未配置 → `in_sync` 等迁移不通知
- 对比通知在通知历史中标记类型（`kind = compare`），可筛选、可手动重发（按当前状态渲染）

## 4. 非功能需求

| 项 | 要求 |
|---|---|
| 部署形态 | 单二进制 + SQLite，无外部依赖；可选 Docker 镜像 |
| 资源占用 | 空载内存 < 100 MB，100 条监控的检查周期内 CPU 占用可忽略 |
| 启动时间 | < 2 秒 |
| 数据持久化 | SQLite（WAL 模式），支持备份/恢复 |
| 并发规模 | 单实例支持 1000 条监控记录 |
| 浏览器兼容 | 最近两个版本的 Chrome / Firefox / Safari |
| 移动端 | 核心操作可用 |
| 国际化 | 中文、英文；界面文案不硬编码 |
| 许可证 | 与依赖兼容，建议 MIT 或 Apache-2.0 |

## 5. 明确的非目标（v1 不做）

- 不做多租户 / 团队协作
- 不做网页视觉选择器（那是 changedetection.io 的领域，与版本检测定位不同）
- 不做插件市场
- 不做集群部署
- 不做移动 App
- 不做桌面通知渠道（服务端通常运行在无头环境，与单二进制零依赖定位冲突；本地提醒用 ntfy/Gotify 即可）
- 不自动修改 PKGBUILD（可列为 v2）

**v2 候选**：
- 「上游版本 vs 本地/AUR 版本」双轨对比：**已规划为 §3.8（M6）**——把同一软件的上游源与发行版源关联，检测落后并在落后出现、差距扩大、追平时提醒。M1–M5 期间以两条独立监控 + 标签的方式过渡
- Webhook 入站（外部触发检查）
- 多用户数据隔离

## 6. 验收标准

1. 全新部署后，通过引导页创建管理员账户，登录进入仪表盘
2. 添加一条 `github_release` 监控，指向 `egoist/mygo-cloudmusic`，点「立即检查」后正确显示 `0.1.19`
3. 添加一条 `aur` 监控，指向 `mygo-cloudmusic`，正确显示 `0.1.19-1`
4. 配置一个 Telegram 渠道，点「发送测试」收到消息
5. 手动把监控记录的当前版本改低，触发检查后产生一条更新记录，并成功推送
6. 配置 OIDC Provider，通过外部 IdP 登录成功，用户自动创建
7. 关闭服务重启后，所有监控、历史、调度计划完整恢复
8. 连续三次检查失败后，监控状态变为 `error`，最后一次成功版本仍保留
9. 导入一份 nvchecker 格式的 TOML（含 GitHub 与 AUR 条目），预览正确、导入后监控可正常检查
10. 删除一个 GitHub 仓库（或监控一个不存在仓库），监控状态变为 `missing`，系统告警渠道收到通知
11. 创建 API Token，用 `curl -H "Authorization: Bearer ..."` 调用 `/api/v1/monitors` 成功；吊销后立即失效
12. 监控设置 `version_ignore_regex` 后，被忽略的版本不产生更新通知
13. 新建 GitHub 与 AUR 两条监控，把 AUR 监控的参照指向 GitHub 监控，两侧检查后显示对比状态；版本 `0.1.19-1` 与 `0.1.19` 判定为一致（pkgrel 对齐生效）
14. 参照监控版本高于跟随监控时，跟随监控进入 `lagging`，其绑定渠道收到一条落后通知；重复检查不重复通知
15. 落后期间参照监控再发新版，收到一条「差距扩大」通知；跟随监控追上后收到「已追平」通知，对比状态回到 `in_sync`
16. 跟随监控或参照监控处于 `error`/`missing` 期间，对比状态保持原值、不产生误报通知
17. 删除参照监控后，跟随监控解除关联、对比字段清空、待投递的对比通知作废，监控本身继续正常检查
18. 从通知历史手动重发一条对比通知，内容按当前对比状态渲染

> 13–18 为 M6 规划项，尚未实现。

## 7. 里程碑建议

| 阶段 | 内容 |
|---|---|
| M1 | 项目骨架、SQLite、认证（本地账户 + API Token）、监控 CRUD |
| M2 | 调度器（含抖动与 per-host 限速）、GitHub/AUR 两种源、检查历史、状态机（含 missing） |
| M3 | 通知渠道（webhook + telegram）、模板、投递记录、静默期与变更周期语义 |
| M4 | OIDC、更多源类型、仪表盘、nvchecker 导入 / JSON 导出 |
| M5 | 指标、Docker 镜像、文档、i18n |
| M6 | 双轨对比（§3.8）、SSE 实时推送（§3.4） |
| M7 | nvchecker 全面兼容与扩展源建设（44 种源全面覆盖、高级版本正则重写、标准 OCI 容器、多候选统一挑选） |
