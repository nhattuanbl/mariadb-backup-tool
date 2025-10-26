# MariaDB Backup Tool

A comprehensive backup solution for MariaDB/MySQL databases with a modern web interface, automated scheduling, and advanced backup management features.

## Features

### 🔄 **Backup Management**
- **Automated Backups**: Full and incremental backup scheduling with intelligent auto-detection
- **Multiple Backup Types**: Full, incremental, and automatic backup mode selection
- **Parallel Processing**: Multi-threaded backup operations for faster performance
- **Compression**: Built-in compression (gzip) to save storage space
- **Retention Management**: Automatic cleanup of old backups with configurable retention policies
- **Database Filtering**: Exclude system databases (information_schema, performance_schema, etc.)
- **Table Optimization**: Optional table optimization after backup completion

### 🌐 **Web Interface**
- **Modern Dashboard**: Responsive web interface with real-time metrics
- **Backup Management**: Start, stop, and monitor backup jobs from the web interface
- **Real-time Monitoring**: Live backup progress, system metrics, and log streaming
- **Settings Management**: Configure all backup parameters through the web interface
- **Log Viewer**: Real-time log streaming with filtering and search capabilities
- **System Metrics**: CPU, memory, disk usage monitoring with charts
- **Backup History**: Complete backup history with status, size, and duration tracking

### 🔧 **Configuration & Management**
- **Flexible Configuration**: Extensive JSON configuration for different environments
- **Command Line Interface**: Full CLI support with multiple command options
- **Password Management**: Secure password hashing and management
- **Database Connection**: Support for TCP, socket, and various authentication methods
- **Custom Binary Paths**: Configurable paths for MariaDB/MySQL tools
- **Backup Scheduling**: Cron-like scheduling with flexible time intervals

### 🔒 **Security & Reliability**
- **Built-in Authentication**: Secure web interface with bcrypt password hashing
- **Systemd Integration**: Native Linux service integration with root privileges
- **Simplified Permissions**: Runs as root for maximum compatibility and simplified setup
- **File Permissions**: Proper file ownership and permission management
- **Error Handling**: Comprehensive error handling and recovery mechanisms
- **Connection Testing**: Automatic database connection validation

### 📊 **Monitoring & Logging**
- **Comprehensive Logging**: Multi-level logging (DEBUG, INFO, WARN, ERROR) with file rotation
- **Log Retention**: Configurable log retention policies
- **Real-time Streaming**: WebSocket-based real-time log streaming
- **System Metrics**: Live system resource monitoring
- **Backup Statistics**: Detailed backup statistics and performance metrics
- **Notification Support**: Slack webhook integration for backup notifications

### 🚀 **Performance & Scalability**
- **Memory Management**: Configurable memory limits and thresholds
- **Process Priority**: Adjustable process priority (nice levels)
- **Resource Monitoring**: Automatic resource usage monitoring
- **Concurrent Operations**: Support for multiple concurrent backup jobs
- **Efficient Storage**: Optimized backup file naming and organization

### 🔧 **Cross-Platform Support**
- **Multi-Platform**: Windows, Linux, macOS support
- **Architecture Support**: AMD64 and ARM64 architectures
- **Easy Installation**: One-command installation script
- **Manual Installation**: Detailed manual installation instructions
- **Service Management**: Systemd service integration on Linux

## Screenshots

### Dashboard Overview
![Dashboard](views/static/images/Screenshot%202025-10-22%20051534.png)

### Backup Management
![Backup Management](views/static/images/Screenshot%202025-10-22%20181223.png)

### Settings Configuration
![Settings](views/static/images/Screenshot%202025-10-22%20181356.png)

### Log Monitoring
![Logs](views/static/images/Screenshot%202025-10-22%20181427.png)

## Quick Start

### Prerequisites

- Linux system with systemd
- MariaDB/MySQL server
- MariaDB client tools (`mariadb-dump`, `mariadb-check`, `mariadb-binlog`)
- Root or sudo access

### Installation

1. **Download and run the installation script:**
   ```bash
   curl -fsSL https://raw.githubusercontent.com/nhattuanbl/mariadb-backup-tool/refs/heads/master/installer.sh | sudo bash
   ```

2. **Configure the application:**
   ```bash
   sudo nano /etc/mariadb-backup-tool/config.json
   ```

3. **Start the service:**
   ```bash
   sudo systemctl start mariadb-backup-tool
   sudo systemctl enable mariadb-backup-tool
   ```

4. **Access the web interface:**
   Open your browser and navigate to `http://localhost:8080`
   - Default username: `admin`
   - Default password: `admin`

## Manual Installation

If you prefer to install manually or the automated script doesn't work, please refer to the **[BUILD.md](BUILD.md)** guide for comprehensive build instructions.

### Building from Source

For detailed instructions on building the MariaDB Backup Tool from source code, including prerequisites, build scripts, and troubleshooting, see: **[BUILD.md](BUILD.md)**

### Quick Manual Installation

### 1. Download the Binary

```bash
# Create installation directory
sudo mkdir -p /etc/mariadb-backup-tool
cd /etc/mariadb-backup-tool

# Download the binary (replace with actual download URL)
sudo wget https://github.com/nhattuanbl/mariadb-backup-tool/releases/latest/download/mariadb-backup-tool-1.0.7-linux-amd64.zip
sudo unzip mariadb-backup-tool-1.0.7-linux-amd64.zip
sudo chmod +x mariadb-backup-tool
```

### 2. Set Root Ownership (No Dedicated User Needed)

```bash
# Set root ownership for simplified permissions
sudo chown -R root:root /etc/mariadb-backup-tool
```

### 3. Create Directories

```bash
# Create necessary directories with root ownership
sudo mkdir -p /etc/mariadb-backup-tool/logs
sudo mkdir -p /etc/mariadb-backup-tool/backups
sudo chown -R root:root /etc/mariadb-backup-tool
```

### 4. Create Configuration

```bash
sudo nano /etc/mariadb-backup-tool/config.json
```

### 5. Create Systemd Service

```bash
# Create systemd service file
sudo tee /etc/systemd/system/mariadb-backup-tool.service > /dev/null << EOF
[Unit]
Description=MariaDB Backup Tool
After=network.target mariadb.service
Wants=mariadb.service

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=/etc/mariadb-backup-tool
ExecStart=/etc/mariadb-backup-tool/mariadb-backup-tool -config=/etc/mariadb-backup-tool/config.json -sqlite=/etc/mariadb-backup-tool/app.db
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=5
StandardOutput=file:/etc/mariadb-backup-tool/logs/output.log
StandardError=file:/etc/mariadb-backup-tool/logs/error.log
SyslogIdentifier=mariadb-backup-tool

LimitNOFILE=65536
LimitNPROC=4096

[Install]
WantedBy=multi-user.target
EOF
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable mariadb-backup-tool
sudo systemctl start mariadb-backup-tool
```

## Usage

### Web Interface

The web interface provides a comprehensive dashboard for managing backups:

1. **Dashboard**: Overview of backup status, system metrics, and recent activity
2. **Backup Management**: Manual backup triggers, backup history, and restore options
3. **Settings**: Configuration management and system settings
4. **Logs**: Real-time log viewing and historical log access
5. **History**: Detailed backup history with download and restore options

### Command Line Arguments

The MariaDB Backup Tool supports the following command-line arguments:

#### `--config` (or `-config`)
- **Description**: Path to the configuration file
- **Default**: `config.json`
- **Example**: `--config /etc/mariadb-backup-tool/config.json`
- **Usage**: Specifies the location of the JSON configuration file containing database, backup, web interface, and other settings.

#### `--sqlite` (or `-sqlite`)
- **Description**: Path to the SQLite database file
- **Default**: `app.db`
- **Example**: `--sqlite /etc/mariadb-backup-tool/app.db`
- **Usage**: Specifies the location of the SQLite database file used for storing backup history, job status, and application state.

#### `--set-password`
- **Description**: Set new password for web interface authentication
- **Default**: Not set (optional)
- **Example**: `--set-password "new_secure_password"`
- **Usage**: Hashes the provided password using bcrypt and updates the `auth_pass_hash` field in the configuration file. The application will exit after successfully updating the password.
- **Security Note**: Use strong passwords and avoid using this argument in scripts or command history.

#### `--version`
- **Description**: Display version information
- **Default**: Not set (optional)
- **Example**: `--version`
- **Usage**: Shows the current version of MariaDB Backup Tool and exits immediately.
- **Output**: `MariaDB Backup Tool v1.0.7`

#### `--help`
- **Description**: Display help information and usage examples
- **Default**: Not set (optional)
- **Example**: `--help`
- **Usage**: Shows comprehensive help information including all available options, descriptions, and usage examples. The application exits immediately after displaying help.

#### `--debug`
- **Description**: Show console window (Windows only)
- **Default**: Not set (optional)
- **Example**: `--debug`
- **Usage**: On Windows, shows the console window to display logs and debug information. On other platforms, this flag has no effect. Useful for troubleshooting and debugging issues.
- **Platform**: Windows only
- **Note**: Without this flag, Windows applications run as GUI applications with hidden console windows for a cleaner user experience.

#### Usage Examples

```bash
# View help and available arguments
./mariadb-backup-tool --help

# Check version information
./mariadb-backup-tool --version

# Start with default configuration
./mariadb-backup-tool

# Start with custom config and database files
./mariadb-backup-tool --config /etc/mariadb-backup-tool/config.json --sqlite /etc/mariadb-backup-tool/app.db

# Run as root to bypass permission issues (recommended for production)
sudo ./mariadb-backup-tool

# Set a new password for the web interface
./mariadb-backup-tool --set-password "my_new_secure_password"

# Set password with custom config file
./mariadb-backup-tool --config /etc/mariadb-backup-tool/config.json --set-password "my_new_secure_password"

# Start with debug console (Windows only)
./mariadb-backup-tool --debug
```

#### Important Notes

- **Password Security**: When using `--set-password`, the password is securely hashed using bcrypt before being stored in the configuration file.
- **Configuration File**: The `--config` argument allows you to use different configuration files for different environments (development, staging, production).
- **Database File**: The `--sqlite` argument allows you to use different SQLite database files, useful for testing or maintaining separate instances.
- **Exit Behavior**: When using `--set-password`, the application will exit immediately after updating the password and will not start the web server.

## Backup Types

### Full Backup
- Complete database dump
- Includes all data and schema
- Takes longer but provides complete restore capability
- Scheduled based on `full_backup_interval`

### Incremental Backup
- Only backs up changes since last backup
- Faster execution
- Requires previous backup for restoration
- Scheduled based on `backup_interval_hours`

### Auto Mode
- Automatically chooses between full and incremental
- Balances performance and completeness
- Default mode for scheduled backups

## Troubleshooting

### Performance Tuning

1. **Adjust parallel processes**: Increase `parallel` setting for faster backups
2. **Memory optimization**: Adjust `max_memory_per_process` based on available RAM
3. **Compression**: Higher compression levels save space but use more CPU
4. **Nice level**: Lower values give backups higher priority

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Changelog

### Version 1.0.7
- Initial release
- Full and incremental backup support
- Web interface with real-time monitoring
- Systemd integration
- Comprehensive configuration options
- Security features and authentication
