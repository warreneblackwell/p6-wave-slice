BINARY_NAME=wavslice
DIST_DIR=dist

.PHONY: all clean darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64 checksums

all: clean darwin-amd64 darwin-arm64 linux-amd64 linux-arm64 windows-amd64 checksums

clean:
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)

darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 main.go

darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 main.go

linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 main.go

linux-arm64:
	GOOS=linux GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 main.go

windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe main.go

checksums:
	@echo "Generating SHA-256 checksums in $(DIST_DIR)/SHA256SUMS"
	@cd $(DIST_DIR) && \
		if command -v sha256sum >/dev/null 2>&1; then \
			sha256sum * > SHA256SUMS; \
		else \
			shasum -a 256 * > SHA256SUMS; \
		fi
