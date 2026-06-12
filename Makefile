# phd — 開発タスクのエントリポイント。CI/リリースは GitHub Actions が同等のことを行う。
BINARY := phd

# ローカルビルドにも version/commit/date を埋め込む（`./phd version` で確認できる）。
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.DEFAULT_GOAL := build

.PHONY: build
build: ## バイナリをビルド（version 埋め込み）
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

.PHONY: test
test: ## ユニットテスト
	go test ./...

.PHONY: fmt
fmt: ## gofmt で整形
	gofmt -w .

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: check
check: fmt vet test ## fmt + vet + test（push 前の一括チェック）

.PHONY: demo
demo: build ## docs/demo の GIF を VHS で再生成（要 vhs / フェイクデータ docs/demo/fixture.json を使用）
	vhs docs/demo/cli.tape
	vhs docs/demo/tui.tape
	vhs docs/demo/tui-filter.tape

.PHONY: snapshot
snapshot: ## goreleaser でローカルにリリース成果物を生成（タグ不要・公開しない）
	goreleaser release --snapshot --clean

.PHONY: release-check
release-check: ## .goreleaser.yaml の妥当性を検証
	goreleaser check

.PHONY: clean
clean: ## ビルド成果物を削除
	rm -rf $(BINARY) dist/

.PHONY: help
help: ## このヘルプを表示
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
