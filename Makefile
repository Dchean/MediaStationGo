.PHONY: all build build-server build-web dev dev-web run deploy docker docker-stop docker-push clean install-web tidy test vet smoke

# ---- 默认目标 ----
all: build

# ---- 全量构建(前端 + 后端) ----
build: build-web build-server

# ---- 后端构建 ----
# 使用纯 Go SQLite,可以 CGO_ENABLED=0 直接交叉编译。
build-server:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/mediastation-go ./cmd/server

# ---- 前端构建 ----
build-web:
	cd web && npm install --silent && npm run build

# ---- 开发模式 ----
dev:
	MEDIASTATION_APP_DEBUG=true go run ./cmd/server

dev-web:
	cd web && npm run dev

# ---- 一键运行 ----
run: build
	./bin/mediastation-go

deploy:
	./scripts/deploy.sh

# ---- Docker ----
docker:
	docker compose up -d

docker-stop:
	docker compose down

docker-push:
	./scripts/docker-build-push.sh

# ---- 工具命令 ----
install-web:
	cd web && npm install

tidy:
	go mod tidy

test:
	go test ./...

vet:
	go vet ./...

# ---- 端到端冒烟测试 ----
# Spins up the binary against a temp dir, exercises every REST surface,
# tears it back down. Fails the build on any regression.
smoke: build
	./scripts/smoke-test.sh

clean:
	rm -rf bin/ web/dist/ web/node_modules/
