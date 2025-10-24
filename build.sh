#!/bin/bash
set -e

# Read version from version.txt
VERSION=$(cat version.txt)
BUILD_DIR="build"
DIST_DIR="dist"

echo "Building MariaDB Backup Tool v${VERSION}..."

# Clean previous builds
rm -rf $BUILD_DIR $DIST_DIR
mkdir -p $BUILD_DIR $DIST_DIR

# Build for all platforms
echo "Building for Linux AMD64..."
GOOS=linux GOARCH=amd64 go build -o $BUILD_DIR/mariadb-backup-tool-linux-amd64 -ldflags="-s -w -X main.Version=${VERSION}" -trimpath main.go

echo "Building for Linux ARM64..."
GOOS=linux GOARCH=arm64 go build -o $BUILD_DIR/mariadb-backup-tool-linux-arm64 -ldflags="-s -w -X main.Version=${VERSION}" -trimpath main.go

echo "Building for macOS AMD64..."
GOOS=darwin GOARCH=amd64 go build -o $BUILD_DIR/mariadb-backup-tool-darwin-amd64 -ldflags="-s -w -X main.Version=${VERSION}" -trimpath main.go

echo "Building for macOS ARM64..."
GOOS=darwin GOARCH=arm64 go build -o $BUILD_DIR/mariadb-backup-tool-darwin-arm64 -ldflags="-s -w -X main.Version=${VERSION}" -trimpath main.go

echo "Building for Windows AMD64..."
GOOS=windows GOARCH=amd64 go build -o $BUILD_DIR/mariadb-backup-tool-windows-amd64.exe -ldflags="-s -w -H windowsgui -X main.Version=${VERSION}" -trimpath main.go

echo "Building for Windows ARM64..."
GOOS=windows GOARCH=arm64 go build -o $BUILD_DIR/mariadb-backup-tool-windows-arm64.exe -ldflags="-s -w -H windowsgui -X main.Version=${VERSION}" -trimpath main.go

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
