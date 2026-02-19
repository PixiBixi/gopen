.PHONY: build install clean

# Build for current platform
build:
	go build -o gopen main.go

# Build for all platforms
build-all:
	GOOS=darwin GOARCH=amd64 go build -o gopen-darwin-amd64 main.go
	GOOS=darwin GOARCH=arm64 go build -o gopen-darwin-arm64 main.go
	GOOS=linux GOARCH=amd64 go build -o gopen-linux-amd64 main.go
	GOOS=linux GOARCH=arm64 go build -o gopen-linux-arm64 main.go
	GOOS=windows GOARCH=amd64 go build -o gopen-windows-amd64.exe main.go

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
