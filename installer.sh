#!/bin/bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' 

APP_NAME="mariadb-backup-tool"
APP_VERSION="1.0.4"
INSTALL_DIR="/etc/mariadb-backup-tool"
BIN_DIR="/usr/local/bin"
SYSTEMD_DIR="/etc/systemd/system"
CONFIG_DIR="/etc/mariadb-backup-tool"
LOG_DIR="/etc/mariadb-backup-tool/logs"
BACKUP_DIR="/etc/mariadb-backup-tool/backups"
USER="root"
GROUP="root"

GITHUB_REPO="nhattuanbl/mariadb-backup-tool"
GITHUB_URL="https://github.com/${GITHUB_REPO}"

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        print_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

detect_system() {
    print_status "Detecting system information..."
    
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        OS=$NAME
        OS_VERSION=$VERSION_ID
    else
        OS=$(uname -s)
        OS_VERSION=$(uname -r)
    fi
    
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        armv7l)
            ARCH="arm"
            ;;
        *)
            print_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac
    
    print_status "OS: $OS $OS_VERSION"
    print_status "Architecture: $ARCH"
}

check_dependencies() {
    print_status "Checking dependencies..."
    
    local missing_deps=()
    
    if ! command -v wget &> /dev/null && ! command -v curl &> /dev/null; then
        missing_deps+=("wget or curl")
    fi
    
    if ! command -v tar &> /dev/null; then
        missing_deps+=("tar")
    fi
    
    if ! command -v systemctl &> /dev/null; then
        print_error "systemd is required but not found"
        exit 1
    fi
    
    if [[ ${#missing_deps[@]} -gt 0 ]]; then
        print_error "Missing dependencies: ${missing_deps[*]}"
        print_status "Please install the missing dependencies and run the script again"
        exit 1
    fi
    
    print_success "All dependencies are available"
}

create_user() {
    print_status "Running as root user (no dedicated user needed)..."
    print_success "Using root user for maximum permissions and simplified setup"
}

install_binary() {
    print_status "Downloading MariaDB Backup Tool..."
    
    # Check if service was running before installation
    SERVICE_WAS_RUNNING=false
    if systemctl is-active --quiet mariadb-backup-tool 2>/dev/null; then
        SERVICE_WAS_RUNNING=true
    fi
    
    TEMP_DIR=$(mktemp -d)
    cd $TEMP_DIR
    
    local download_url=""
    local binary_name=""
    
    if command -v curl &> /dev/null; then
        print_status "Fetching latest release information..."
        API_RESPONSE=$(curl -s "https://api.github.com/repos/${GITHUB_REPO}/releases/latest")
        
        if [[ $? -ne 0 ]]; then
            print_error "Failed to fetch release information from GitHub API"
            print_status "API URL: https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
            exit 1
        fi
        
        LATEST_RELEASE=$(echo "$API_RESPONSE" | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)
        
        if [[ -z "$LATEST_RELEASE" ]]; then
            print_warning "No releases found in GitHub API response"
            print_status "API Response: $API_RESPONSE"
        else
            # Remove 'v' prefix from version for filename
            VERSION_NUMBER=${LATEST_RELEASE#v}
            download_url="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_RELEASE}/mariadb-backup-tool-${VERSION_NUMBER}-linux-${ARCH}.zip"
            binary_name="mariadb-backup-tool-${VERSION_NUMBER}-linux-${ARCH}.zip"
        fi
    else
        print_error "curl is required to fetch release information but not found"
        exit 1
    fi
    
    if [[ -z "$download_url" ]]; then
        print_warning "No release found for MariaDB Backup Tool"
        print_status "Attempted to fetch latest release from: https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
        print_status "Please build the binary manually following the BUILD.md tutorial:"
        print_status "1. Clone the repository: git clone https://github.com/${GITHUB_REPO}.git"
        print_status "2. Follow the build instructions in BUILD.md"
        print_status "3. Run this installer again after building"
        print_error "Installation aborted. Please build the binary first."
        exit 1
    fi
    
    print_status "Found release: $LATEST_RELEASE"
    print_status "Download URL: $download_url"
    
    # Skip URL accessibility test - proceed directly to download
    print_status "Proceeding with download..."
    
    print_status "Downloading binary..."
    DOWNLOAD_SUCCESS=false
    
    # Try wget first if available
    if command -v wget &> /dev/null; then
        print_status "Using wget to download..."
        if wget -v "$download_url" -O "$binary_name" 2>&1; then
            DOWNLOAD_SUCCESS=true
        else
            print_warning "wget failed, trying curl..."
        fi
    fi
    
    # Try curl if wget failed or is not available
    if [[ "$DOWNLOAD_SUCCESS" == "false" ]] && command -v curl &> /dev/null; then
        print_status "Using curl to download..."
        if curl -vL "$download_url" -o "$binary_name" 2>&1; then
            DOWNLOAD_SUCCESS=true
        else
            print_error "Both wget and curl failed to download the binary"
        fi
    fi
    
    if [[ "$DOWNLOAD_SUCCESS" == "false" ]]; then
        print_error "Failed to download binary using available tools"
        print_status "URL: $download_url"
        print_status "Please check your internet connection and try again"
        print_status "You can also manually download the file and place it in the current directory"
        exit 1
    fi
    
    if [[ ! -f "$binary_name" ]]; then
        print_error "Downloaded file not found: $binary_name"
        print_status "Download URL: $download_url"
        exit 1
    fi
    
    # Check if downloaded file is not empty
    if [[ ! -s "$binary_name" ]]; then
        print_error "Downloaded file is empty"
        print_status "This might indicate a 404 error or network issue"
        exit 1
    fi
    
    print_success "Binary downloaded successfully"
    
    print_status "Extracting binary..."
    if ! unzip -q "$binary_name"; then
        print_error "Failed to extract the downloaded archive"
        print_status "Archive: $binary_name"
        exit 1
    fi
    
    # Look for the binary file (it might have the platform suffix)
    BINARY_FILE=""
    if [[ -f "mariadb-backup-tool" ]]; then
        BINARY_FILE="mariadb-backup-tool"
    elif [[ -f "mariadb-backup-tool-linux-amd64" ]]; then
        BINARY_FILE="mariadb-backup-tool-linux-amd64"
    elif [[ -f "mariadb-backup-tool-linux-arm64" ]]; then
        BINARY_FILE="mariadb-backup-tool-linux-arm64"
    else
        print_error "Binary file not found in extracted archive"
        print_status "Contents of extracted archive:"
        ls -la
        exit 1
    fi
    
    print_status "Found binary: $BINARY_FILE"
    
    print_status "Installing binary..."
    mkdir -p $INSTALL_DIR
    
    # Check if service is running and stop it
    if systemctl is-active --quiet mariadb-backup-tool 2>/dev/null; then
        print_status "Stopping running MariaDB Backup Tool service..."
        systemctl stop mariadb-backup-tool
        sleep 2  # Give it time to stop
    fi
    
    # Check if binary exists and is in use
    if [[ -f "$INSTALL_DIR/mariadb-backup-tool" ]]; then
        print_status "Removing existing binary..."
        rm -f "$INSTALL_DIR/mariadb-backup-tool"
    fi
    
    if ! cp "$BINARY_FILE" $INSTALL_DIR/mariadb-backup-tool; then
        print_error "Failed to copy binary to installation directory"
        print_status "Source: $BINARY_FILE"
        print_status "Destination: $INSTALL_DIR/mariadb-backup-tool"
        exit 1
    fi
    
    if ! chmod +x $INSTALL_DIR/mariadb-backup-tool; then
        print_error "Failed to make binary executable"
        exit 1
    fi
    
    if ! ln -sf $INSTALL_DIR/mariadb-backup-tool $BIN_DIR/mariadb-backup-tool; then
        print_error "Failed to create symlink"
        print_status "Source: $INSTALL_DIR/mariadb-backup-tool"
        print_status "Destination: $BIN_DIR/mariadb-backup-tool"
        exit 1
    fi
    
    cd /
    rm -rf $TEMP_DIR
    
    print_success "Binary installed successfully"
    
    # Restart service if it was running before
    if [[ "$SERVICE_WAS_RUNNING" == "true" ]]; then
        print_status "Restarting MariaDB Backup Tool service..."
        systemctl start mariadb-backup-tool
        if systemctl is-active --quiet mariadb-backup-tool 2>/dev/null; then
            print_success "Service restarted successfully"
        else
            print_warning "Service failed to restart. Please check manually: sudo systemctl status mariadb-backup-tool"
        fi
    fi
}

create_directories() {
    print_status "Creating directories..."
    
    mkdir -p $CONFIG_DIR
    mkdir -p $LOG_DIR
    mkdir -p $BACKUP_DIR
    
    chown root:root $CONFIG_DIR
    chown root:root $LOG_DIR
    chown root:root $BACKUP_DIR
    chmod 755 $CONFIG_DIR
    chmod 755 $LOG_DIR
    chmod 755 $BACKUP_DIR
    
    chown -R root:root $INSTALL_DIR
    chmod 755 $INSTALL_DIR
    
    print_success "Directories created successfully"
}

# Note: Configuration file will be auto-created by the application with sensible defaults

create_systemd_service() {
    print_status "Creating systemd service file..."
    
    cat > $SYSTEMD_DIR/mariadb-backup-tool.service << EOF
[Unit]
Description=MariaDB Backup Tool
Documentation=https://github.com/${GITHUB_REPO}
After=network.target mariadb.service
Wants=mariadb.service

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=${CONFIG_DIR}
ExecStart=${INSTALL_DIR}/mariadb-backup-tool -config=${CONFIG_DIR}/config.json -sqlite=${CONFIG_DIR}/app.db
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=5
StandardOutput=file:${LOG_DIR}/output.log
StandardError=file:${LOG_DIR}/error.log
SyslogIdentifier=mariadb-backup-tool

LimitNOFILE=65536
LimitNPROC=4096

[Install]
WantedBy=multi-user.target
EOF
    
    systemctl daemon-reload
    
    print_success "Systemd service file created"
}

install_mariadb_tools() {
    print_status "Checking for MariaDB client tools..."
    
    local missing_tools=()
    
    if ! command -v mariadb-dump &> /dev/null; then
        missing_tools+=("mariadb-dump")
    fi
    
    if ! command -v mariadb-check &> /dev/null; then
        missing_tools+=("mariadb-check")
    fi
    
    if ! command -v mariadb-binlog &> /dev/null; then
        missing_tools+=("mariadb-binlog")
    fi
    
    if [[ ${#missing_tools[@]} -gt 0 ]]; then
        print_warning "Missing MariaDB tools: ${missing_tools[*]}"
        print_status "Please install MariaDB client tools:"
        
        if command -v apt-get &> /dev/null; then
            print_status "  sudo apt-get update && sudo apt-get install mariadb-client"
        elif command -v yum &> /dev/null; then
            print_status "  sudo yum install MariaDB-client"
        elif command -v dnf &> /dev/null; then
            print_status "  sudo dnf install mariadb"
        elif command -v zypper &> /dev/null; then
            print_status "  sudo zypper install mariadb-client"
        else
            print_status "  Install MariaDB client package for your distribution"
        fi
    else
        print_success "MariaDB client tools are available"
    fi
}

show_summary() {
    print_success "Installation completed successfully!"
    echo
    echo "Installation Summary:"
    echo "===================="
    echo "Binary location: $INSTALL_DIR/mariadb-backup-tool"
    echo "Symlink: $BIN_DIR/mariadb-backup-tool"
    echo "Configuration: $CONFIG_DIR/config.json"
    echo "Logs: $LOG_DIR"
    echo "Backups: $BACKUP_DIR"
    echo "Service: mariadb-backup-tool.service"
    echo "User: root (no dedicated user needed)"
    echo "Group: root (simplified permissions)"
    echo
    echo "Next Steps:"
    echo "==========="
    echo "1. Configuration file will be auto-created on first run"
    echo "   Edit it after starting the service:"
    echo "   sudo nano $CONFIG_DIR/config.json"
    echo
    echo "2. Update database credentials and backup settings"
    echo
    echo "3. Start the service:"
    echo "   sudo systemctl start mariadb-backup-tool"
    echo
    echo "4. Enable auto-start on boot:"
    echo "   sudo systemctl enable mariadb-backup-tool"
    echo
    echo "5. Check service status:"
    echo "   sudo systemctl status mariadb-backup-tool"
    echo
    echo "6. Access the web interface:"
    echo "   http://localhost:8080 (default credentials: admin/admin)"
    echo
    echo "7. View logs:"
    echo "   sudo journalctl -u mariadb-backup-tool -f"
    echo
    print_warning "IMPORTANT: Update the default password in the configuration file!"
}

main() {
    echo "MariaDB Backup Tool Installation Script"
    echo "======================================="
    echo
    
    check_root
    detect_system
    check_dependencies
    create_user
    install_binary
    create_directories
    create_systemd_service
    install_mariadb_tools
    show_summary
}

main "$@"
