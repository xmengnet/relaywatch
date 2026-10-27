.PHONY: build web backend dev dev-backend dev-web test clean

build: web backend

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

backend:
	go build -trimpath -ldflags="-s -w -X main.release=$(shell git describe --tags 2>/dev/null || echo dev)" -o relaywatch ./cmd/relaywatch

# dev: 一条命令同时拉起后端与前端 Vite(:5173,已代理 /api 和 /auth)。
# 浏览器访问 http://localhost:5173（HMR），API 由 Vite 转发到后端。
# 后端用 -tags dev 跳过 go:embed，无需先构建前端产物。Ctrl-C 同时退出两者。
# 退出时按进程树遍历后代精准发 TERM：pnpm 会把 vite 拆到独立进程组，
# 终端 Ctrl-C 的组广播和 "kill 0" 都够不到它，必须点名杀。
dev:
	@test -f config.yaml || { echo "缺 config.yaml：cp config.example.yaml config.yaml 并填 secret_key"; exit 1; }
	@bash -c 'set -m; desc() { local q="$$1" list="" p n; \
		while [ -n "$$q" ]; do n=""; for p in $$q; do [ -n "$$p" ] || continue; \
			list="$$list $$p"; n="$$n $$(pgrep -P $$p 2>/dev/null)"; done; q="$$n"; done; echo "$$list"; }; \
		cleanup() { local pids="$$(desc $$BE_PID) $$(desc $$WEB_PID)"; \
			kill -TERM $$pids 2>/dev/null; wait 2>/dev/null; \
			kill -KILL $$pids 2>/dev/null; return 0; }; \
		trap cleanup INT TERM EXIT; \
		$(MAKE) --no-print-directory dev-backend & BE_PID=$$!; \
		$(MAKE) --no-print-directory dev-web & WEB_PID=$$!; \
		wait'

dev-backend:
	@if [ -z "$$RELAYWATCH_SECRET_KEY" ] && grep -q 'secret_key: ""' config.yaml 2>/dev/null; then \
		echo "未设置 secret_key：openssl rand -base64 32 生成后填入 config.yaml，或 export RELAYWATCH_SECRET_KEY"; \
		exit 1; \
	fi
	go run -tags dev ./cmd/relaywatch -config config.yaml

dev-web:
	@$(eval BACKEND_ADDR := $(shell grep -E '^\s+addr:' config.yaml 2>/dev/null | awk '{print $$2}' | head -1))
	@if [ -n "$(BACKEND_ADDR)" ]; then export RW_BACKEND="http://localhost$(BACKEND_ADDR)"; fi; \
	cd web && pnpm dev

test:
	go test ./...
	cd web && pnpm test

clean:
	rm -rf relaywatch web/dist
