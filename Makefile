# Root Makefile — Wails app lives in cmd/rabbit (wails.json + main.go)
SHELL := /bin/bash
.ONESHELL:
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

APP_DIR       := cmd/rabbit
FRONTEND_DIR  := frontend
ASSETS_DST    := $(APP_DIR)/frontend/dist
WAILS         := wails

.PHONY: help run build gui clean format test frontend assets

help:
	@echo "Targets:"
	@echo "  run       - wails dev from $(APP_DIR)"
	@echo "  build     - build SPA, stage assets, then wails build"
	@echo "  gui       - run the built app (macOS .app or binary)"
	@echo "  clean     - clean Go cache and Wails output + staged assets"
	@echo "  format    - golines + gofmt + go mod tidy"
	@echo "  test      - go test ./... (race)"
	@echo "  frontend  - npm ci && npm run build (to $(FRONTEND_DIR)/dist)"
	@echo "  assets    - copy dist/ into $(ASSETS_DST) for go:embed"

run:
	cd "$(APP_DIR)" && "$(WAILS)" dev

build: frontend assets
	cd "$(APP_DIR)" && "$(WAILS)" build

frontend:
	npm --prefix "$(FRONTEND_DIR)" ci --no-audit --no-fund
	npm --prefix "$(FRONTEND_DIR)" run build

assets:
	rm -rf "$(ASSETS_DST)"
	mkdir -p "$(APP_DIR)/frontend"
	cp -R "$(FRONTEND_DIR)/dist" "$(ASSETS_DST)"

gui: build
	@if [ -d "$(APP_DIR)/build/bin/rabbit.app" ]; then \
	  "$(APP_DIR)/build/bin/rabbit.app/Contents/MacOS/rabbit"; \
	elif [ -x "$(APP_DIR)/build/bin/rabbit" ]; then \
	  "$(APP_DIR)/build/bin/rabbit"; \
	elif [ -x "$(APP_DIR)/build/bin/rabbit.exe" ]; then \
	  "$(APP_DIR)/build/bin/rabbit.exe"; \
	else \
	  echo "No built binary found under $(APP_DIR)/build/bin"; exit 1; \
	fi

clean:
	go clean
	@if [ -d "$(APP_DIR)/build" ]; then rm -rf "$(APP_DIR)/build"; fi
	@if [ -d "$(ASSETS_DST)" ]; then rm -rf "$(ASSETS_DST)"; fi
	cd "$(APP_DIR)" && "$(WAILS)" build -clean || true

format:
	golines -m 100 -t 8 --shorten-comments -w .
	gofmt -s -w .
	go mod tidy

test:
	go test ./... -race -count=1
