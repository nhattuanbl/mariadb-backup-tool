package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"golang.org/x/crypto/bcrypt"
)

// Version will be injected at build time via -ldflags
var Version = "dev"

func main() {
	configFile := flag.String("config", "config.json", "Path to configuration file")
	sqliteFile := flag.String("sqlite", "app.db", "Path to SQLite database file")
	setPassword := flag.String("set-password", "", "Set new password for web interface authentication")
	showVersion := flag.Bool("version", false, "Show version information")
	showHelp := flag.Bool("help", false, "Show help information")
	debugMode := flag.Bool("debug", false, "Show console window (Windows only)")
	flag.Parse()

	// Handle console window on Windows
	handleWindowsConsole(*debugMode)

	// Handle help display
	if *showHelp {
		fmt.Println("MariaDB Backup Tool - A comprehensive backup solution for MariaDB/MySQL databases")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  mariadb-backup-tool [options]")
		fmt.Println()
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  mariadb-backup-tool                                    # Start with default settings")
		fmt.Println("  mariadb-backup-tool --config /etc/mbt/config.json      # Use custom config file")
		fmt.Println("  mariadb-backup-tool --set-password newpassword        # Set new web interface password")
		fmt.Println("  mariadb-backup-tool --version                          # Show version information")
		fmt.Println("  mariadb-backup-tool --debug                            # Show console window (Windows only)")
		os.Exit(0)
	}

	// Handle version display
	if *showVersion {
		fmt.Printf("MariaDB Backup Tool v%s\n", Version)
		os.Exit(0)
	}

	// Handle password setting
	if *setPassword != "" {
		if err := setNewPassword(*configFile, *setPassword); err != nil {
			log.Fatalf("Failed to set password: %v", err)
		}
		fmt.Println("Password updated successfully!")
		os.Exit(0)
	}

	config, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := InitializeLogger(config); err != nil {
		log.Fatalf("Failed to initialize logging system: %v", err)
	}

	// Setup direct log broadcasting to WebSocket clients
	SetupLogBroadcasting()

	LogInfo("MariaDB Backup Tool starting up...")
	LogDebug("Config file: %s", *configFile)
	LogDebug("SQLite file: %s", *sqliteFile)

	if err := InitDB(*sqliteFile); err != nil {
		LogError("Failed to initialize SQLite: %v", err)
		log.Fatalf("Failed to initialize SQLite: %v", err)
	}
	LogInfo("SQLite database initialized successfully")

	go autoTestConnectionsOnStart(config)
	go startSystemMetricsBroadcaster()
	go startJobsBroadcaster()
	go StartScheduler(config)
	setupRoutes(config)

	// Start system tray on Windows
	StartTrayIfWindows(config)

	addr := fmt.Sprintf(":%d", config.Web.Port)
	LogInfo("Starting MariaDB Backup Tool on http://localhost%s", addr)
	LogDebug("Web server configuration - Port: %d, SSL: %v", config.Web.Port, config.Web.SSLEnabled)

	if err := http.ListenAndServe(addr, nil); err != nil {
		LogError("Server failed to start: %v", err)
		log.Fatalf("Server failed to start: %v", err)
	}
}

// setNewPassword hashes a new password and updates the config file
func setNewPassword(configFile, newPassword string) error {
	// Load existing config
	config, err := loadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	// Hash the new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %v", err)
	}

	// Update the password hash in config
	config.Web.AuthPassHash = string(hashedPassword)

	// Save the updated config
	if err := saveConfig(config, configFile); err != nil {
		return fmt.Errorf("failed to save config: %v", err)
	}

	return nil
}
