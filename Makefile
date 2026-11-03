.PHONY: version check-version bump-version build-web prepare-embed build-server build-servers build-linux build-release clean dev run

# 变量定义
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/dushixiang/uart_sms_forwarder/internal/version.Version=$(VERSION)
GOFLAGS := CGO_ENABLED=0

# 打印当前版本号
version:
	@python3 scripts/check_version.py --print

# 检查版本号一致性（前端 / 后端 / CHANGELOG 三处）
check-version:
	@python3 scripts/check_version.py

# 发版改号：make bump-version V=1.2.0
bump-version:
	@if [ -z "$(V)" ]; then echo "用法：make bump-version V=1.2.0"; exit 1; fi
	@python3 scripts/check_version.py --bump $(V)

# 构建前端
build-web: check-version
	@echo "Building web frontend..."
	cd web && yarn && yarn build
	@echo "Web frontend built successfully!"

# 同步前端产物到 go:embed 目录
# web/assets.go 里 embed 的是 web/embed_dir（保证干净 clone 也能编译）。
# 存在 web/dist 时用真实产物覆盖它；不存在时保留占位页。
prepare-embed:
	@if [ -f web/dist/index.html ]; then \
		echo "同步 web/dist -> web/embed_dir ..."; \
		rm -rf web/embed_dir && cp -a web/dist web/embed_dir; \
		echo "已嵌入真实前端产物"; \
	else \
		echo "⚠️  web/dist 不存在，使用占位页（后端 API 仍可用）"; \
		echo "    如需前端界面：cd web && npm ci && npm run build"; \
	fi

# 构建服务端（单平台 - Linux amd64）
build-server: prepare-embed check-version
	@echo "Building server for Linux amd64..."
	@mkdir -p bin
	$(GOFLAGS) GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-linux-amd64 ./cmd/serv
	upx bin/uart_sms_forwarder-linux-amd64 || true
	@echo "Server built successfully!"
	@ls -lh bin/

# 构建服务端（多平台）
# ⚠️ 必须依赖 prepare-embed：漏了它 → npm run build 产出的 web/dist
#    没进 embed_dir，打出来的二进制内嵌的是「仓库里的旧前端」。
build-servers: prepare-embed check-version
	@echo "Building servers for multiple platforms..."
	@mkdir -p bin

	# Linux
	@echo "Building for Linux amd64..."
	$(GOFLAGS) GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-linux-amd64 ./cmd/serv

	@echo "Building for Linux arm64..."
	$(GOFLAGS) GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-linux-arm64 ./cmd/serv

	@echo "Building for Linux arm..."
	$(GOFLAGS) GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-linux-arm ./cmd/serv

	# Windows
	@echo "Building for Windows amd64..."
	$(GOFLAGS) GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-windows-amd64.exe ./cmd/serv

	@echo "Building for Windows arm64..."
	$(GOFLAGS) GOOS=windows GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-windows-arm64.exe ./cmd/serv

	# macOS
	@echo "Building for macOS amd64..."
	$(GOFLAGS) GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-darwin-amd64 ./cmd/serv

	@echo "Building for macOS arm64..."
	$(GOFLAGS) GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-darwin-arm64 ./cmd/serv

	# FreeBSD
	@echo "Building for FreeBSD amd64..."
	$(GOFLAGS) GOOS=freebsd GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-freebsd-amd64 ./cmd/serv

	@echo "Compressing binaries..."
	upx bin/uart_sms_forwarder-linux-* bin/uart_sms_forwarder-windows-* bin/uart_sms_forwarder-freebsd-* 2>/dev/null || true

	@echo "All servers built successfully!"
	@ls -lh bin/

# 构建 Linux 平台（用于 Docker 镜像）
build-linux: prepare-embed
	@echo "Building for Linux platforms (Docker)..."
	@mkdir -p bin

	# Linux amd64
	@echo "Building for Linux amd64..."
	$(GOFLAGS) GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-linux-amd64 ./cmd/serv
	upx bin/uart_sms_forwarder-linux-amd64

	# Linux arm64
	@echo "Building for Linux arm64..."
	$(GOFLAGS) GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/uart_sms_forwarder-linux-arm64 ./cmd/serv
	upx bin/uart_sms_forwarder-linux-arm64

	@echo "Linux binaries built successfully!"
	@ls -lh bin/

# 构建所有（发布版本）
build-release:
	@echo "Building release version..."
	make build-web
	make build-server
	@echo "Release build completed!"

# 清理构建文件
clean:
	@echo "Cleaning build files..."
	rm -rf bin
	rm -rf web/dist
	@echo "Clean completed!"

# 开发构建（不执行 upx 压缩，但仍需先构建前端：
# web/assets.go 用 go:embed 引用 web/dist，缺 dist 会编译失败。
# 若只想要占位页，可跳过 build-web —— dist_placeholder 会兜底）
dev:
	@echo "Building for development..."
	@mkdir -p bin
	go build -o bin/uart_sms_forwarder ./cmd/serv
	@echo "Development build completed!"
	@ls -lh bin/

# 运行（开发模式）
run:
	@echo "Running in development mode..."
	go run ./cmd/serv

# 默认目标
build: build-release
