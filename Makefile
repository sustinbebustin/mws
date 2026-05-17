.PHONY: build test lint vuln ci install tidy clean

GO := GOTOOLCHAIN=auto go

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
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Mirror of the .github/workflows/ci.yml jobs. Run before pushing.
ci: build test lint vuln

install:
	$(GO) install ./cmd/mws

tidy:
	$(GO) mod tidy

clean:
	$(GO) clean ./...
