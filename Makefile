GO ?= go
BUN ?= bun
CONFIG ?= config.yaml
BINARY ?= cli-proxy-api
SERVER_PACKAGE ?= ./cmd/server
WEB_DIR ?= web
ARGS ?=

.DEFAULT_GOAL := help

.PHONY: help config dev dev-backend run build test fmt verify web-install web-build web-dev web-test \
	codex-login codex-device-login claude-login antigravity-login kimi-login xai-login

help:
	@printf '%s\n' \
		'make dev                  构建管理页并使用本地模型目录启动服务' \
		'make dev-backend          仅启动后端，配合 make web-dev 使用' \
		'make web-dev              启动支持热更新的 Web 管理页' \
		'make web-build            构建 Web 管理页单文件产物' \
		'make run                  启动开发服务并允许更新模型目录' \
		'make build                构建包含 Web 管理页的服务端二进制文件' \
		'make test                 运行前端和后端全部测试' \
		'make fmt                  格式化 Go 代码' \
		'make verify               执行必需的编译验证' \
		'make codex-login          登录 Codex OAuth' \
		'make codex-device-login   使用设备码登录 Codex OAuth' \
		'make claude-login         登录 Claude OAuth' \
		'make antigravity-login    登录 Antigravity OAuth' \
		'make kimi-login           登录 Kimi OAuth' \
		'make xai-login            登录 xAI OAuth' \
		'' \
		'可覆盖参数：CONFIG、BINARY、SERVER_PACKAGE、WEB_DIR、ARGS'

config:
	@if [ ! -f "$(CONFIG)" ]; then \
		cp config.example.yaml "$(CONFIG)"; \
		printf '已创建配置文件：%s\n' "$(CONFIG)"; \
	fi

web-install:
	@command -v "$(BUN)" >/dev/null 2>&1 || { printf '未找到 Bun，请先安装 Bun 1.3.14+。\n' >&2; exit 1; }
	cd "$(WEB_DIR)" && "$(BUN)" install --frozen-lockfile

web-build: web-install
	cd "$(WEB_DIR)" && "$(BUN)" run build

web-test: web-install
	cd "$(WEB_DIR)" && "$(BUN)" run test

web-dev: web-install
	cd "$(WEB_DIR)" && "$(BUN)" run dev -- --host 0.0.0.0

dev: config web-build
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" --local-model $(ARGS)

dev-backend: config
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" --local-model $(ARGS)

run: config web-build
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" $(ARGS)

build: web-build
	$(GO) build -o "$(BINARY)" $(SERVER_PACKAGE)

test: web-test
	$(GO) test ./...

fmt:
	$(GO)fmt -w .

verify: web-build
	$(GO) build -o test-output $(SERVER_PACKAGE) && rm test-output

codex-login: config
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" --codex-login $(ARGS)

codex-device-login: config
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" --codex-device-login $(ARGS)

claude-login: config
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" --claude-login $(ARGS)

antigravity-login: config
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" --antigravity-login $(ARGS)

kimi-login: config
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" --kimi-login $(ARGS)

xai-login: config
	$(GO) run $(SERVER_PACKAGE) --config "$(CONFIG)" --xai-login $(ARGS)
