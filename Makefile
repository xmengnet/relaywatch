.PHONY: build web backend dev test clean

build: web backend

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

backend:
	go build -trimpath -ldflags="-s -w -X main.release=$(shell git describe --tags 2>/dev/null || echo dev)" -o relaywatch ./cmd/relaywatch

dev:
	@echo "后端: go run ./cmd/relaywatch"
	@echo "前端: cd web && pnpm dev  (访问 http://localhost:5173)"

test:
	go test ./...
	cd web && pnpm test

clean:
	rm -rf relaywatch web/dist
