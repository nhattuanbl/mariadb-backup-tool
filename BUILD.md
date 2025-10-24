# MariaDB Backup Tool - Build Guide

This guide will help you build the MariaDB Backup Tool from source code.

## Prerequisites

### Required Software

1. **Go** (version 1.19 or later)
   - Download from: https://golang.org/dl/
   - Or install via package manager (see below)

2. **Git**
   - Download from: https://git-scm.com/downloads
   - Or install via package manager (see below)

3. **MariaDB Client Tools** (for testing)
   - `mariadb-dump`
   - `mariadb-check`
   - `mariadb-binlog`

### Installing Prerequisites

#### Ubuntu/Debian
```bash
sudo apt-get update
sudo apt-get install golang-go git mariadb-client
```

#### CentOS/RHEL/Fedora
```bash
# For CentOS/RHEL
sudo yum install golang git mariadb

# For Fedora
sudo dnf install golang git mariadb
```

#### macOS
```bash
# Using Homebrew
brew install go git mariadb

# Or download Go from https://golang.org/dl/
```

#### Windows
1. Download Go from: https://golang.org/dl/
2. Download Git from: https://git-scm.com/downloads
3. Install MariaDB from: https://mariadb.org/download/

## Building the Application

### 1. Clone the Repository
```bash
git clone https://github.com/nhattuanbl/mariadb-backup-tool.git
cd mariadb-backup-tool
```

### 2. Install Go Dependencies
```bash
go mod download
go mod tidy
```

### 3. Build for Current Platform
```bash
go build -o mariadb-backup-tool main.go
```

### 4. Build for Multiple Platforms

#### Build All Platforms
```bash
# Build for all supported platforms
go build -o build/mariadb-backup-tool-linux-amd64 -ldflags="-s -w" -trimpath main.go
go build -o build/mariadb-backup-tool-linux-arm64 -ldflags="-s -w" -trimpath main.go
go build -o build/mariadb-backup-tool-darwin-amd64 -ldflags="-s -w" -trimpath main.go
go build -o build/mariadb-backup-tool-darwin-arm64 -ldflags="-s -w" -trimpath main.go
go build -o build/mariadb-backup-tool-windows-amd64.exe -ldflags="-s -w" -trimpath main.go
go build -o build/mariadb-backup-tool-windows-arm64.exe -ldflags="-s -w" -trimpath main.go
```

#### Cross-Compilation (if needed)
```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o build/mariadb-backup-tool-linux-amd64 -ldflags="-s -w" -trimpath main.go

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o build/mariadb-backup-tool-linux-arm64 -ldflags="-s -w" -trimpath main.go

# macOS AMD64
GOOS=darwin GOARCH=amd64 go build -o build/mariadb-backup-tool-darwin-amd64 -ldflags="-s -w" -trimpath main.go

# macOS ARM64
GOOS=darwin GOARCH=arm64 go build -o build/mariadb-backup-tool-darwin-arm64 -ldflags="-s -w" -trimpath main.go

# Windows AMD64
GOOS=windows GOARCH=amd64 go build -o build/mariadb-backup-tool-windows-amd64.exe -ldflags="-s -w" -trimpath main.go

# Windows ARM64
GOOS=windows GOARCH=arm64 go build -o build/mariadb-backup-tool-windows-arm64.exe -ldflags="-s -w" -trimpath main.go
```

## Creating Distribution Packages

### 1. Create Distribution Directory
```bash
mkdir -p dist
```

### 2. Package Binaries
```bash
# Linux AMD64
zip dist/mariadb-backup-tool-1.0.2-linux-amd64.zip build/mariadb-backup-tool-linux-amd64

# Linux ARM64
zip dist/mariadb-backup-tool-1.0.2-linux-arm64.zip build/mariadb-backup-tool-linux-arm64

# macOS AMD64
zip dist/mariadb-backup-tool-1.0.2-darwin-amd64.zip build/mariadb-backup-tool-darwin-amd64

# macOS ARM64
zip dist/mariadb-backup-tool-1.0.2-darwin-arm64.zip build/mariadb-backup-tool-darwin-arm64

# Windows AMD64
zip dist/mariadb-backup-tool-1.0.2-windows-amd64.zip build/mariadb-backup-tool-windows-amd64.exe

# Windows ARM64
zip dist/mariadb-backup-tool-1.0.2-windows-arm64.zip build/mariadb-backup-tool-windows-arm64.exe
```

## Testing the Build

### 1. Test the Binary
```bash
# Make sure the binary is executable
chmod +x mariadb-backup-tool

# Test basic functionality
./mariadb-backup-tool --help
```

### 2. Test with Configuration
```bash
# Create a test configuration
./mariadb-backup-tool -config=config.json -sqlite=app.db

# Check if the web interface starts
curl http://localhost:8080
```

## Build Scripts

### Automated Build Script
Create a `build.sh` script for easy building:

```bash
#!/bin/bash
set -e

VERSION="1.0.2"
BUILD_DIR="build"
DIST_DIR="dist"

echo "Building MariaDB Backup Tool v${VERSION}..."

# Clean previous builds
rm -rf $BUILD_DIR $DIST_DIR
mkdir -p $BUILD_DIR $DIST_DIR

# Build for all platforms
echo "Building for Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -o $BUILD_DIR/mariadb-backup-tool-linux-amd64 -ldflags="-s -w" -trimpath main.go

echo "Building for Linux ARM64..."
GOOS=linux GOARCH=arm64 go build -o $BUILD_DIR/mariadb-backup-tool-linux-arm64 -ldflags="-s -w" -trimpath main.go

echo "Building for macOS AMD64..."
GOOS=darwin GOARCH=amd64 go build -o $BUILD_DIR/mariadb-backup-tool-darwin-amd64 -ldflags="-s -w" -trimpath main.go

echo "Building for macOS ARM64..."
GOOS=darwin GOARCH=arm64 go build -o $BUILD_DIR/mariadb-backup-tool-darwin-arm64 -ldflags="-s -w" -trimpath main.go

echo "Building for Windows AMD64..."
GOOS=windows GOARCH=amd64 go build -o $BUILD_DIR/mariadb-backup-tool-windows-amd64.exe -ldflags="-s -w" -trimpath main.go

echo "Building for Windows ARM64..."
GOOS=windows GOARCH=arm64 go build -o $BUILD_DIR/mariadb-backup-tool-windows-arm64.exe -ldflags="-s -w" -trimpath main.go

# Create distribution packages
echo "Creating distribution packages..."
zip $DIST_DIR/mariadb-backup-tool-${VERSION}-linux-amd64.zip $BUILD_DIR/mariadb-backup-tool-linux-amd64
zip $DIST_DIR/mariadb-backup-tool-${VERSION}-linux-arm64.zip $BUILD_DIR/mariadb-backup-tool-linux-arm64
zip $DIST_DIR/mariadb-backup-tool-${VERSION}-darwin-amd64.zip $BUILD_DIR/mariadb-backup-tool-darwin-amd64
zip $DIST_DIR/mariadb-backup-tool-${VERSION}-darwin-arm64.zip $BUILD_DIR/mariadb-backup-tool-darwin-arm64
zip $DIST_DIR/mariadb-backup-tool-${VERSION}-windows-amd64.zip $BUILD_DIR/mariadb-backup-tool-windows-amd64.exe
zip $DIST_DIR/mariadb-backup-tool-${VERSION}-windows-arm64.zip $BUILD_DIR/mariadb-backup-tool-windows-arm64.exe

echo "Build completed! Binaries are in $BUILD_DIR/"
echo "Distribution packages are in $DIST_DIR/"
```

Make it executable:
```bash
chmod +x build.sh
./build.sh
```

## Troubleshooting

### Common Issues

#### 1. Go Version Issues
```bash
# Check Go version
go version

# If version is too old, update Go
# Download from https://golang.org/dl/
```

#### 2. Module Issues
```bash
# Clean module cache
go clean -modcache

# Re-download dependencies
go mod download
go mod tidy
```

#### 3. Permission Issues
```bash
# Make sure you have write permissions
chmod +x mariadb-backup-tool
```

#### 4. Missing Dependencies
```bash
# Check if all dependencies are available
go list -m all
```

### Build Flags Explained

- `-ldflags="-s -w"`: Strip debug information and symbol table (reduces binary size)
- `-trimpath`: Remove file system paths from the resulting executable
- `-o`: Output file name

## Development

### Running in Development Mode
```bash
# Run with verbose logging
go run main.go -config=config.json -sqlite=app.db -debug

# Run with specific port
go run main.go -config=config.json -sqlite=app.db -port=8080
```

### Code Formatting
```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run
```

### Testing
```bash
# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...
```

## Creating a Release

### 1. Tag the Release
```bash
git tag v1.0.2
git push origin v1.0.2
```

### 2. Create GitHub Release
Use the provided `create-release.sh` script:
```bash
./create-release.sh
```

Or manually create a release on GitHub and upload the distribution packages from the `dist/` directory.

## Installation After Building

After building the binary, you can:

1. **Use the installer script** (recommended):
   ```bash
   curl -fsSL https://raw.githubusercontent.com/nhattuanbl/mariadb-backup-tool/refs/heads/master/installer.sh | sudo bash
   ```

2. **Manual installation**:
   ```bash
   sudo cp mariadb-backup-tool /usr/local/bin/
   sudo chmod +x /usr/local/bin/mariadb-backup-tool
   ```

3. **Run directly**:
   ```bash
   ./mariadb-backup-tool -config=config.json -sqlite=app.db
   ```

## Support

If you encounter issues during the build process:

1. Check the [Issues](https://github.com/nhattuanbl/mariadb-backup-tool/issues) page
2. Ensure you have the correct Go version
3. Verify all dependencies are installed
4. Check the build logs for specific error messages

For additional help, please refer to the main [README.md](README.md) file.
