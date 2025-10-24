@echo off
setlocal enabledelayedexpansion

set VERSION=1.0.0
set BUILD_DIR=build
set DIST_DIR=dist

echo Building MariaDB Backup Tool v%VERSION%...

REM Clean previous builds
if exist %BUILD_DIR% rmdir /s /q %BUILD_DIR%
if exist %DIST_DIR% rmdir /s /q %DIST_DIR%
mkdir %BUILD_DIR%
mkdir %DIST_DIR%

REM Build for all platforms
echo Building for Linux AMD64...
set GOOS=linux
set GOARCH=amd64
go build -o %BUILD_DIR%\mariadb-backup-tool-linux-amd64 -ldflags="-s -w" -trimpath .
if errorlevel 1 (
    echo Failed to build for Linux AMD64
    exit /b 1
)

echo Building for Linux ARM64...
set GOOS=linux
set GOARCH=arm64
go build -o %BUILD_DIR%\mariadb-backup-tool-linux-arm64 -ldflags="-s -w" -trimpath .
if errorlevel 1 (
    echo Failed to build for Linux ARM64
    exit /b 1
)

echo Building for macOS AMD64...
set GOOS=darwin
set GOARCH=amd64
go build -o %BUILD_DIR%\mariadb-backup-tool-darwin-amd64 -ldflags="-s -w" -trimpath .
if errorlevel 1 (
    echo Failed to build for macOS AMD64
    exit /b 1
)

echo Building for macOS ARM64...
set GOOS=darwin
set GOARCH=arm64
go build -o %BUILD_DIR%\mariadb-backup-tool-darwin-arm64 -ldflags="-s -w" -trimpath .
if errorlevel 1 (
    echo Failed to build for macOS ARM64
    exit /b 1
)

echo Building for Windows AMD64...
set GOOS=windows
set GOARCH=amd64
go build -o %BUILD_DIR%\mariadb-backup-tool-windows-amd64.exe -ldflags="-s -w -H windowsgui" -trimpath .
if errorlevel 1 (
    echo Failed to build for Windows AMD64
    exit /b 1
)

echo Building for Windows ARM64...
set GOOS=windows
set GOARCH=arm64
go build -o %BUILD_DIR%\mariadb-backup-tool-windows-arm64.exe -ldflags="-s -w -H windowsgui" -trimpath .
if errorlevel 1 (
    echo Failed to build for Windows ARM64
    exit /b 1
)

REM Create distribution packages
echo Creating distribution packages...

REM Check if PowerShell is available for zip compression
where powershell >nul 2>nul
if errorlevel 1 (
    echo PowerShell not found. Please install PowerShell or use 7-Zip to create zip files.
    echo Binaries are ready in %BUILD_DIR%\ directory.
    pause
    exit /b 0
)

echo Creating Linux AMD64 package...
powershell -Command "Compress-Archive -Path '%BUILD_DIR%\mariadb-backup-tool-linux-amd64' -DestinationPath '%DIST_DIR%\mariadb-backup-tool-%VERSION%-linux-amd64.zip' -Force"

echo Creating Linux ARM64 package...
powershell -Command "Compress-Archive -Path '%BUILD_DIR%\mariadb-backup-tool-linux-arm64' -DestinationPath '%DIST_DIR%\mariadb-backup-tool-%VERSION%-linux-arm64.zip' -Force"

echo Creating macOS AMD64 package...
powershell -Command "Compress-Archive -Path '%BUILD_DIR%\mariadb-backup-tool-darwin-amd64' -DestinationPath '%DIST_DIR%\mariadb-backup-tool-%VERSION%-darwin-amd64.zip' -Force"

echo Creating macOS ARM64 package...
powershell -Command "Compress-Archive -Path '%BUILD_DIR%\mariadb-backup-tool-darwin-arm64' -DestinationPath '%DIST_DIR%\mariadb-backup-tool-%VERSION%-darwin-arm64.zip' -Force"

echo Creating Windows AMD64 package...
powershell -Command "Compress-Archive -Path '%BUILD_DIR%\mariadb-backup-tool-windows-amd64.exe' -DestinationPath '%DIST_DIR%\mariadb-backup-tool-%VERSION%-windows-amd64.zip' -Force"

echo Creating Windows ARM64 package...
powershell -Command "Compress-Archive -Path '%BUILD_DIR%\mariadb-backup-tool-windows-arm64.exe' -DestinationPath '%DIST_DIR%\mariadb-backup-tool-%VERSION%-windows-arm64.zip' -Force"

echo.
echo Build completed successfully!
echo Binaries are in %BUILD_DIR%\ directory.
echo Distribution packages are in %DIST_DIR%\ directory.
echo.
echo Files created:
dir %BUILD_DIR%\ /b
echo.
dir %DIST_DIR%\ /b
echo.
pause
