BUF_VERSION ?= v1.65.0
PLAYWRIGHT_GO_VERSION ?= v0.5700.1

.PHONY: setup generate playwright-install playwright-install-ci test test-all test-unit test-unit-strict test-unit-ci test-integration test-e2e

setup:
	@echo "Installing project dependencies..."
	go mod download

	@echo "Installing goimports..."
	go install golang.org/x/tools/cmd/goimports@latest

	@echo "Installing gofumpt..."
	go install mvdan.cc/gofumpt@latest

# Ensuring same version of golangci-lint is used locally if dev uses it and in CI
	@echo "Installing golangci-lint..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4


	@echo "Installing and configuring Lefthook..."
	go install github.com/evilmartians/lefthook@v1.13.6
	lefthook install

	@echo "Installing buf..."
	go install github.com/bufbuild/buf/cmd/buf@v1.65.0

	@echo "Updating buf dependencies and generating code..."
	buf dep update
	buf generate

	@echo "Setup complete! You are ready to code."

generate:
	go run github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION) generate

playwright-install:
	go run github.com/playwright-community/playwright-go/cmd/playwright@$(PLAYWRIGHT_GO_VERSION) install chromium

playwright-install-ci:
	go run github.com/playwright-community/playwright-go/cmd/playwright@$(PLAYWRIGHT_GO_VERSION) install --with-deps chromium

test: test-unit

test-all: test-unit-strict test-integration test-e2e

test-unit:
	go test ./...

test-unit-strict:
	go test -skip '^TestIntegration_' ./...

test-unit-ci:
	go test -race -count=1 -skip '^TestIntegration_' ./...

test-integration:
	go test -count=1 -tags=integration -run '^TestIntegration_' ./...

test-e2e:
	go test -count=1 -tags=e2e -run '^TestIndexPageSubscriptionFlow$$' ./cmd/server
