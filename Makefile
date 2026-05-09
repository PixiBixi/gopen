.PHONY: build install clean

LDFLAGS := -s -w
BUILD_FLAGS := -trimpath -ldflags="$(LDFLAGS)"

# Build for current platform
build:
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -o gopen .

# Build for all platforms
build-all:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(BUILD_FLAGS) -o gopen-darwin-amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(BUILD_FLAGS) -o gopen-darwin-arm64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o gopen-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -o gopen-linux-arm64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(BUILD_FLAGS) -o gopen-windows-amd64.exe .

# Install to /usr/local/bin (requires sudo)
install: build
	sudo mv gopen /usr/local/bin/gopen
	sudo chmod +x /usr/local/bin/gopen

# Install to ~/bin (no sudo required)
install-user: build
	mkdir -p ~/bin
	mv gopen ~/bin/gopen
	chmod +x ~/bin/gopen

# Clean build artifacts
clean:
	rm -f gopen gopen-*
