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