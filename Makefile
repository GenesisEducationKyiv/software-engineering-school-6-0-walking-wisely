setup:
	@echo "Installing project dependencies..."
	go mod download

	@echo "Installing goimports..."
	go install golang.org/x/tools/cmd/goimports@latest

	@echo "Installing and configuring Lefthook..."
	go install github.com/evilmartians/lefthook@latest
	lefthook install

	@echo "Installing buf..."
	go install github.com/bufbuild/buf/cmd/buf@v1.67.0

	@echo "Updating buf dependencies and generating code..."
	buf dep update
	buf generate

	@echo "Setup complete! You are ready to code."