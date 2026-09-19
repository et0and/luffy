# Luffy build targets.
#
# The original PirateBox ran on a TP-Link TL-MR3020 (Atheros AR9331,
# big-endian MIPS 24Kc, no hardware FPU). That target is:
#
#	make mips
#
# which produces a static, soft-float binary suitable for OpenWrt's
# mips_24kc packages.

BINARY  := luffy
VERSION ?= 1.0.0
GO      ?= go
LDFLAGS := -s -w -X main.version=$(VERSION)
PKG     := ./cmd/luffy

.PHONY: all build test vet fmt clean dist mips mipsle arm arm64 amd64

all: build

## Host build for local development.
build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

## Static Linux binaries for router class hardware.
mips:
	CGO_ENABLED=0 GOOS=linux GOARCH=mips   GOMIPS=softfloat $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-mips-softfloat $(PKG)

mipsle:
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-mipsle-softfloat $(PKG)

arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-armv6 $(PKG)

arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 $(PKG)

amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 $(PKG)

## Build every supported target.
dist: mips mipsle arm arm64 amd64

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -l -w .

clean:
	rm -rf dist $(BINARY)
