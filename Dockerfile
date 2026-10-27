# 前端构建
FROM node:22-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# 后端构建
FROM golang:1.27-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relaywatch ./cmd/relaywatch

# 运行
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend /relaywatch /usr/local/bin/relaywatch
VOLUME /data
EXPOSE 8080
# RELAYWATCH_SECRET_KEY 必须通过环境变量（-e）或挂载的 config.yaml 提供
ENTRYPOINT ["relaywatch", "-config", "/data/config.yaml"]
