# git-memory (Go). `make build` for this machine, `make release` for the Linux and Windows binaries.
VERSION ?= dev
PKG     := git-memory/internal/gm
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION)
TARGETS := linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build test release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/git-memory ./cmd/git-memory

test:
	go vet ./...
	go test ./...

# release/git-memory_<os>_<arch>[.exe] plus SHA256SUMS. Static binaries (CGO off): nothing to install next to them.
release:
	mkdir -p release
	for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = windows ] && ext=".exe"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o release/git-memory_$${os}_$${arch}$$ext ./cmd/git-memory || exit 1; \
	done
	cd release && sha256sum git-memory_* > SHA256SUMS
