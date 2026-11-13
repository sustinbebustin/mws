.PHONY: build test lint vuln ci install tidy clean hooks hooks-test

GO := GOTOOLCHAIN=auto go

# Skeleton hook sources ship their module file as go.mod.tmpl, since go:embed
# skips any directory holding a go.mod. Each hook is vetted, tested and built
# from a temp copy where the file is restored to go.mod.
HOOKS := $(patsubst %/go.mod.tmpl,%,$(wildcard skeleton/.mws/.claude/hooks/*/go.mod.tmpl))

define each-hook
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; set -e; \
	for h in $(HOOKS); do \
		mod="$$tmp/$$(basename $$h)"; \
		cp -R "$$h" "$$mod"; mv "$$mod/go.mod.tmpl" "$$mod/go.mod"; \
		echo "$$h"; (cd "$$mod" && $(1)); \
	done
endef

build:
	$(GO) build ./...

test:
	$(GO) test ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not installed."; \
		echo "  install: https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

hooks-test:
	$(call each-hook,$(GO) vet ./... && $(GO) test ./...)

# Rebuilds the committed <hook>-bin files the skeleton's settings.json runs.
hooks: hooks-test
	$(call each-hook,CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags='-s -w' -o "$(CURDIR)/$$h-bin" .)

# Mirror of the .github/workflows/ci.yml jobs. Run before pushing.
ci: build test hooks-test lint vuln

install:
	$(GO) install ./cmd/mws

tidy:
	$(GO) mod tidy

clean:
	$(GO) clean ./...
