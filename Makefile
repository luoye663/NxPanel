# NxPanel Makefile
# 提供常用的构建、测试、运行命令

# Go 相关变量
GO          ?= go
GOFLAGS     ?= -v
BINARY_API  = bin/nxpanel-api
BINARY_AGENT= bin/nxpanel-agent

# 前端相关变量
WEBAPP_DIR  = webapp-react
PNPM        ?= pnpm
CONFIG_FILE ?= configs/config.yaml
FRONTEND_GATE_PATH ?= $(shell awk 'BEGIN{inapi=0} /^api:/ {inapi=1; next} /^[^[:space:]#][^:]*:/ {inapi=0} inapi && /^[[:space:]]*login_path:/ {value=$$0; sub(/^[^:]*:[[:space:]]*/, "", value); gsub(/["'\'' ]/, "", value); print value; exit}' $(CONFIG_FILE) 2>/dev/null)

# Docker 相关变量
DOCKER_NS        ?= ghcr.io/luoye663/nxpanel
DOCKERHUB_NS     ?= docker.io/luoye663/nxpanel
DOCKER_PLATFORMS ?= linux/amd64,linux/arm64
DOCKERHUB_PLATFORM ?= linux/amd64
GHCR_PLATFORM ?= linux/amd64
GHCR_USERNAME ?= $(GITHUB_ACTOR)

# 版本号（可通过 -ldflags 注入）
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
LDFLAGS     = -s -w -X github.com/luoye663/nxpanel/internal/app.Version=$(VERSION)

# 构建目标
.PHONY: all build build-api build-agent build-frontend clean test run-api run-agent lint fmt vet tidy help upload-release test-install-compat test-install-compat-full docker-build-nginx docker-build-openresty docker-build docker-multiarch docker-login-ghcr docker-push docker-push-dockerhub docker-push-dockerhub-amd64 docker-push-dockerhub-arm64 docker-push-dockerhub-multiarch docker-push-ghcr docker-push-ghcr-amd64 docker-push-ghcr-arm64 docker-push-ghcr-multiarch docker-push-all

# 默认目标：构建全部（含前端）
all: build-frontend build

## build: 构建 API 和 Agent 二进制（需先 build-frontend）
build: build-api build-agent

build-api:
	@echo "构建 nxpanel-api..."
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_API) ./cmd/nxpanel-api

build-agent:
	@echo "构建 nxpanel-agent..."
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_AGENT) ./cmd/nxpanel-agent

## build-frontend: 构建前端 React 应用 → web/dist
build-frontend:
	@echo "构建 React 前端..."
	cd $(WEBAPP_DIR) && $(PNPM) run build

## build-all: 完整构建（前端 + 后端）
build-all: build-frontend build

## docker-build-nginx: 构建 nginx 变体镜像（独享模式，含 nginx+agent+api）
docker-build-nginx:
	docker build -f docker/Dockerfile.nginx --build-arg VERSION=$(VERSION) \
		-t nxpanel:nginx-$(VERSION) -t nxpanel:nginx-latest .

## docker-build-openresty: 构建 openresty 变体镜像（独享模式，含 openresty+agent+api）
docker-build-openresty:
	docker build -f docker/Dockerfile.openresty --build-arg VERSION=$(VERSION) \
		-t nxpanel:openresty-$(VERSION) -t nxpanel:openresty-latest .

## docker-build: 构建两个变体镜像（本地，单架构）
docker-build: docker-build-nginx docker-build-openresty

## docker-multiarch: 多架构构建（amd64+arm64），需 docker buildx；仅加载到本地
docker-multiarch:
	docker buildx build --platform $(DOCKER_PLATFORMS) --build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile.nginx -t $(DOCKER_NS):nginx-$(VERSION) -t $(DOCKER_NS):nginx-latest \
		--load .
	docker buildx build --platform $(DOCKER_PLATFORMS) --build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile.openresty -t $(DOCKER_NS):openresty-$(VERSION) -t $(DOCKER_NS):openresty-latest \
		--load .

## docker-login-ghcr: 使用 GHCR_TOKEN 安全登录 GHCR（不在命令行暴露 token）
##   用法：GHCR_TOKEN=... GHCR_USERNAME=<github_username> make docker-login-ghcr
docker-login-ghcr:
	@if [ -z "$(GHCR_USERNAME)" ]; then \
		echo "GHCR_USERNAME 未设置，请执行：GHCR_TOKEN=<github_token> GHCR_USERNAME=<github_username> make docker-login-ghcr"; \
		exit 1; \
	fi
	@if [ -z "$$GHCR_TOKEN" ]; then \
		echo "GHCR_TOKEN 未设置，请先通过环境变量传入 GitHub PAT"; \
		exit 1; \
	fi
	@printf '%s\n' "$$GHCR_TOKEN" | docker login ghcr.io -u "$(GHCR_USERNAME)" --password-stdin

## docker-push: 默认构建 amd64 并推送到 Docker Hub
##   需要：docker login docker.io
docker-push: docker-push-dockerhub

## docker-push-dockerhub: 构建指定平台并只推送到 Docker Hub（默认 linux/amd64）
docker-push-dockerhub:
	@echo "==> 构建并推送 nginx 变体到 Docker Hub（$(DOCKERHUB_PLATFORM)）"
	docker buildx build --push --platform $(DOCKERHUB_PLATFORM) --build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile.nginx \
		-t $(DOCKERHUB_NS):nginx-$(VERSION) -t $(DOCKERHUB_NS):nginx-latest .
	@echo "==> 构建并推送 openresty 变体到 Docker Hub（$(DOCKERHUB_PLATFORM)）"
	docker buildx build --push --platform $(DOCKERHUB_PLATFORM) --build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile.openresty \
		-t $(DOCKERHUB_NS):openresty-$(VERSION) -t $(DOCKERHUB_NS):openresty-latest .
	@echo "==> Docker Hub 推送完成"

## docker-push-dockerhub-amd64: 构建 linux/amd64 并只推送到 Docker Hub
docker-push-dockerhub-amd64:
	$(MAKE) docker-push-dockerhub DOCKERHUB_PLATFORM=linux/amd64

## docker-push-dockerhub-arm64: 构建 linux/arm64 并只推送到 Docker Hub
docker-push-dockerhub-arm64:
	$(MAKE) docker-push-dockerhub DOCKERHUB_PLATFORM=linux/arm64

## docker-push-dockerhub-multiarch: 多架构构建并只推送到 Docker Hub
docker-push-dockerhub-multiarch:
	$(MAKE) docker-push-dockerhub DOCKERHUB_PLATFORM=$(DOCKER_PLATFORMS)

## docker-push-ghcr: 构建指定平台并只推送到 GHCR（默认 linux/amd64）
##   需要：docker login ghcr.io
docker-push-ghcr:
	@echo "==> 构建并推送 nginx 变体到 GHCR（$(GHCR_PLATFORM)）"
	docker buildx build --push --platform $(GHCR_PLATFORM) --build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile.nginx \
		-t $(DOCKER_NS):nginx-$(VERSION) -t $(DOCKER_NS):nginx-latest .
	@echo "==> 构建并推送 openresty 变体到 GHCR（$(GHCR_PLATFORM)）"
	docker buildx build --push --platform $(GHCR_PLATFORM) --build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile.openresty \
		-t $(DOCKER_NS):openresty-$(VERSION) -t $(DOCKER_NS):openresty-latest .
	@echo "==> GHCR 推送完成"

## docker-push-ghcr-amd64: 构建 linux/amd64 并只推送到 GHCR
docker-push-ghcr-amd64:
	$(MAKE) docker-push-ghcr GHCR_PLATFORM=linux/amd64

## docker-push-ghcr-arm64: 构建 linux/arm64 并只推送到 GHCR
docker-push-ghcr-arm64:
	$(MAKE) docker-push-ghcr GHCR_PLATFORM=linux/arm64

## docker-push-ghcr-multiarch: 多架构构建并只推送到 GHCR
docker-push-ghcr-multiarch:
	$(MAKE) docker-push-ghcr GHCR_PLATFORM=$(DOCKER_PLATFORMS)

## docker-push-all: 多架构构建并同时推送到 Docker Hub 和 GHCR
##   需要：docker login docker.io && docker login ghcr.io
docker-push-all:
	@echo "==> 构建并推送 nginx 变体到 Docker Hub + GHCR（$(DOCKER_PLATFORMS)）"
	docker buildx build --push --platform $(DOCKER_PLATFORMS) --build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile.nginx \
		-t $(DOCKERHUB_NS):nginx-$(VERSION) -t $(DOCKERHUB_NS):nginx-latest \
		-t $(DOCKER_NS):nginx-$(VERSION) -t $(DOCKER_NS):nginx-latest .
	@echo "==> 构建并推送 openresty 变体到 Docker Hub + GHCR（$(DOCKER_PLATFORMS)）"
	docker buildx build --push --platform $(DOCKER_PLATFORMS) --build-arg VERSION=$(VERSION) \
		-f docker/Dockerfile.openresty \
		-t $(DOCKERHUB_NS):openresty-$(VERSION) -t $(DOCKERHUB_NS):openresty-latest \
		-t $(DOCKER_NS):openresty-$(VERSION) -t $(DOCKER_NS):openresty-latest .
	@echo "==> Docker Hub + GHCR 推送完成"

## clean: 清理构建产物
clean:
	@echo "清理构建产物..."
	rm -rf bin/
	rm -rf web/dist/
	$(GO) clean

## test: 运行所有后端测试
test:
	$(GO) test ./... -v -count=1

## run-api: 运行 API 服务（开发模式）
run-api: build-api
	./$(BINARY_API) -config configs/config.yaml

## run-agent: 运行 Agent 服务（开发模式）
run-agent: build-agent
	./$(BINARY_AGENT) -config configs/config.yaml

## dev-frontend: 前端开发模式（热更新 + API 代理）
dev-frontend:
	@if [ -z "$(FRONTEND_GATE_PATH)" ]; then \
		echo "未从 $(CONFIG_FILE) 读取到 api.login_path；请先启动 API 生成入口，或手动执行 VITE_NX_GATE_PATH=/nx-xxx make dev-frontend"; \
		exit 1; \
	fi
	cd $(WEBAPP_DIR) && VITE_NX_GATE_PATH=$(FRONTEND_GATE_PATH) $(PNPM) run dev

## lint: 运行代码检查
lint:
	golangci-lint run ./...

## fmt: 格式化代码
fmt:
	gofmt -w .
	goimports -w .

## vet: 静态分析
vet:
	$(GO) vet ./...

## tidy: 整理依赖
tidy:
	$(GO) mod tidy

## release: 生成发布包
release: build-frontend build
	@echo "生成发布包..."
	@rm -rf release/
	@mkdir -p release/nxpanel/bin release/nxpanel/web release/nxpanel/configs/templates release/nxpanel/configs/nginx release/nxpanel/scripts
	@install -m 755 $(BINARY_API) release/nxpanel/bin/
	@install -m 755 $(BINARY_AGENT) release/nxpanel/bin/
	@install -m 644 LICENSE release/nxpanel/
	@install -m 644 configs/config.example.yaml release/nxpanel/configs/
	@cp -r web/dist/* release/nxpanel/web/
	@cp -r configs/templates/* release/nxpanel/configs/templates/
	@cp configs/nginx/nginx.conf.production release/nxpanel/configs/nginx/
	@cp configs/nginx/nginx.conf.openresty release/nxpanel/configs/nginx/
	@cp scripts/nxpanel-api.service scripts/nxpanel-agent.service scripts/nginx.service scripts/openresty.service release/nxpanel/scripts/
	@cp -r scripts/nginx-install release/nxpanel/scripts/
	@chmod +x release/nxpanel/scripts/nginx-install/install.sh
	@cp install.sh release/nxpanel/
	@cp upgrade.sh release/nxpanel/
	@cp uninstall.sh release/nxpanel/
	@chmod +x release/nxpanel/install.sh
	@chmod +x release/nxpanel/upgrade.sh
	@chmod +x release/nxpanel/uninstall.sh
	@echo "生成校验文件..."
	@cd release/nxpanel && \
		echo "# nxpanel $(VERSION)" > checksums.txt && \
		echo "# generated: $$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> checksums.txt && \
		sha256sum bin/nxpanel-api >> checksums.txt && \
		sha256sum bin/nxpanel-agent >> checksums.txt && \
		find configs/templates -type f -exec sha256sum {} \; >> checksums.txt
	@cd release && tar -czf nxpanel-linux-amd64.tar.gz nxpanel/
	@echo "发布包已生成: release/nxpanel-linux-amd64.tar.gz"

## upload-release: 上传到 GitHub Release（需要 gh CLI）
upload-release: release
	gh release create $(VERSION) \
		release/nxpanel-linux-amd64.tar.gz \
		--title "$(VERSION)"

## test-install-compat: 测试 scripts/nginx-install/install.sh 在各发行版的兼容性
test-install-compat:
	bash test-install-compat.sh

## test-install-compat-full: 测试完整 install.sh 在各发行版的兼容性
test-install-compat-full:
	bash test-install-compat.sh --full

## help: 显示帮助信息
help:
	@echo "NxPanel Makefile"
	@echo ""
	@echo "可用目标:"
	@echo "  all              构建前端 + 后端（默认）"
	@echo "  build            构建 API 和 Agent 二进制"
	@echo "  build-api        仅构建 API"
	@echo "  build-agent      仅构建 Agent"
	@echo "  build-frontend   构建前端 React 应用 → web/dist"
	@echo "  build-all        完整构建（前端 + 后端）"
	@echo "  build-docker      （已移除）使用 docker-build-nginx / docker-build-openresty"
	@echo "  docker-build-nginx     构建 nginx 变体镜像（独享模式）"
	@echo "  docker-build-openresty 构建 openresty 变体镜像（独享模式）"
	@echo "  docker-build           构建两个变体镜像（本地，单架构）"
	@echo "  docker-multiarch       多架构构建（amd64+arm64），加载到本地"
	@echo "  docker-login-ghcr      使用 GHCR_TOKEN 安全登录 GHCR"
	@echo "  docker-push            构建 amd64 并推送到 Docker Hub（默认）"
	@echo "  docker-push-dockerhub  构建指定平台并只推送到 Docker Hub（默认 amd64）"
	@echo "  docker-push-dockerhub-amd64       构建 linux/amd64 并只推送到 Docker Hub"
	@echo "  docker-push-dockerhub-arm64       构建 linux/arm64 并只推送到 Docker Hub"
	@echo "  docker-push-dockerhub-multiarch   多架构构建并只推送到 Docker Hub"
	@echo "  docker-push-ghcr       构建指定平台并只推送到 GHCR（默认 amd64）"
	@echo "  docker-push-ghcr-amd64       构建 linux/amd64 并只推送到 GHCR"
	@echo "  docker-push-ghcr-arm64       构建 linux/arm64 并只推送到 GHCR"
	@echo "  docker-push-ghcr-multiarch   多架构构建并只推送到 GHCR"
	@echo "  docker-push-all        多架构构建并推送到 Docker Hub + GHCR"
	@echo "  clean            清理构建产物"
	@echo "  test             运行所有后端测试"
	@echo "  run-api          运行 API 服务"
	@echo "  run-agent        运行 Agent 服务"
	@echo "  dev-frontend     前端开发模式（热更新）"
	@echo "  lint             运行代码检查"
	@echo "  fmt              格式化代码"
	@echo "  vet              静态分析"
	@echo "  tidy             整理依赖"
	@echo "  install          安装到系统路径"
	@echo "  release          生成发布包（tar.gz）"
	@echo "  upload-release   上传到 GitHub Release（需要 gh CLI）"
	@echo "  test-install-compat     测试 scripts/nginx-install/install.sh 在各发行版的兼容性（Docker）"
	@echo "  test-install-compat-full 测试完整 install.sh 在各发行版的兼容性（Docker + systemd）"
