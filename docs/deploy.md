# 部署

## 构建产物形态

`make build` 产出单个可执行文件 `relaywatch`（前端已 `go:embed` 打包），运行时零依赖。构建环境需要 Go 1.27+、Node 22+、pnpm；运行环境无任何要求。

```bash
make build
./relaywatch -config config.yaml
```

## 初始化

1. 生成加密密钥并写入配置（**必做**，未设置拒绝启动）：

   ```bash
   openssl rand -base64 32
   ```

2. 复制 `config.example.yaml` 为 `config.yaml`，填入 `security.secret_key`、`server.addr`、`database.path`。

3. 启动后浏览器打开 `http://<addr>`，首次访问引导创建管理员账户（仅用户表为空时可用）。

4. **备份 `secret_key`**。密钥丢失则数据库中所有 token / webhook 配置无法解密恢复。密钥轮换：新密钥写入 `security.secret_key`，旧密钥放 `security.old_secret_key`，服务会自动用旧密钥解密存量数据（读时回退）。

## Docker

```bash
docker build -t relaywatch .

docker run -d --name relaywatch \
  -p 8080:8080 \
  -v relaywatch-data:/data \
  -e RELAYWATCH_SECRET_KEY="$(openssl rand -base64 32)" \
  relaywatch
```

数据（SQLite + WAL）在 `/data` 卷内；把 `config.yaml` 放进挂载目录或改用环境变量。`RELAYWATCH_SECRET_KEY` 优先于配置文件。

注意：容器重建后若 `RELAYWATCH_SECRET_KEY` 是每次随机生成的，已加密配置将无法解密——生产环境固定一个密钥并妥善保管。

## systemd

```bash
sudo useradd -r -s /usr/sbin/nologin -d /var/lib/relaywatch relaywatch
sudo mkdir -p /var/lib/relaywatch /etc/relaywatch
sudo cp relaywatch /usr/local/bin/
sudo cp config.example.yaml /etc/relaywatch/config.yaml
sudo cp deploy/relaywatch.env.example /etc/relaywatch/env
sudo chown -R relaywatch:relaywatch /var/lib/relaywatch
sudo chmod 600 /etc/relaywatch/env   # 编辑 env 填入 RELAYWATCH_SECRET_KEY

sudo cp deploy/relaywatch.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now relaywatch
```

unit 文件已含 `ProtectSystem=strict`、`NoNewPrivileges` 等加固，写路径仅 `/var/lib/relaywatch`。

## 反向代理

服务自带 SPA fallback 与安全响应头。前面挂 Nginx 时：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}

# SSE 需要关闭缓冲（M6 实时更新启用时）
location /api/v1/events {
    proxy_pass http://127.0.0.1:8080;
    proxy_buffering off;
    proxy_cache off;
    proxy_set_header Connection '';
    proxy_http_version 1.1;
    chunked_transfer_encoding off;
}

# MCP 端点：同样是 SSE 长连接，需关闭缓冲并放宽读超时
location /mcp {
    proxy_pass http://127.0.0.1:8080;
    proxy_buffering off;
    proxy_cache off;
    proxy_set_header Connection '';
    proxy_http_version 1.1;
    proxy_read_timeout 1h;
}
```

`server.base_url` 需设为外部可达地址（`https://relaywatch.example.com`），OIDC 回调依赖它。

反向代理部署还需在 `config.yaml` 设置 `server.trust_proxy: true`，服务才会采信 `X-Real-IP` / `X-Forwarded-For` 作为客户端 IP（登录限流、审计日志依赖它）。直连部署保持 `false`，避免客户端伪造转发头绕过限流。

## Prometheus

`/metrics` 输出文本格式指标（监控按状态计数、启用数、投递队列），无需认证——建议在反代层限制来源，或仅监听内网。

## MCP（AI 客户端接入）

服务内置 MCP 端点 `POST/GET /mcp`（Streamable HTTP），认证复用个人资料页签发的 API Token（`Authorization: Bearer` 头）。工具集为白名单：监控的增改/查/立即检查/确认、渠道发现与创建、告警类型发现、投递排查，不含删除、用户管理与系统配置。

另有公开的 `GET /SKILL.md`（Agent Skill 格式，含接入指引与 REST 速查）：AI 客户端（Claude Code / VS Code 等）抓取该 URL 即可自行安装 skill，例如「帮我安装 https://relaywatch.example.com/SKILL.md 这个 skill」。skill 本身不含凭据——AI 首次使用时会向用户索取 Base URL 与 API Token。

客户端接入示例：

```bash
# Claude Code
claude mcp add --transport http relaywatch https://relaywatch.example.com/mcp \
  --header "Authorization: Bearer <API Token>"

# 验证
claude mcp list
```

其他客户端（Cursor / Codex 等）：URL 填 `https://你的域名/mcp`，请求头加 `Authorization: Bearer <API Token>`。Token 在 Web 端「个人资料 → API Token」创建，吊销即时生效。

## 备份

- 数据库：`sqlite3 data/relaywatch.db ".backup backup.db"`（在线安全备份），或直接复制 `.db` 文件（WAL 模式下建议先停写）
- 加密密钥：与数据库分开保管
