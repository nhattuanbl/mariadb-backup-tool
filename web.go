package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"golang.org/x/crypto/bcrypt"
)

//go:embed views
var viewsFS embed.FS

var templates *template.Template

var testState = struct {
	ConnectionStatus  string
	ConnectionMessage string
	BinaryStatus      string
	BinaryMessage     string
	BinlogFormat      string
	BinlogPath        string
	LastTested        time.Time
	ButtonsEnabled    bool
}{
	ConnectionStatus:  "unknown",
	ConnectionMessage: "",
	BinaryStatus:      "unknown",
	BinaryMessage:     "",
	BinlogFormat:      "",
	BinlogPath:        "",
	LastTested:        time.Time{},
	ButtonsEnabled:    false,
}

var sessions = make(map[string]time.Time)
var sessionsMutex sync.RWMutex
var sessionTimeout = 24 * time.Hour

func init() {
	// Load HTML templates from embedded filesystem
	var err error
	templates, err = template.ParseFS(viewsFS, "views/*.html")
	if err != nil {
		LogWarn("Failed to load templates: %v", err)
	}
}

func setupRoutes(config *Config) {
	// Create a custom file server with proper MIME types for embedded files
	staticHandler := http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get the file path from the request
		filePath := r.URL.Path
		if !strings.HasPrefix(filePath, "/") {
			filePath = "/" + filePath
		}

		// Construct the full path in the embedded filesystem
		fullPath := "views/static" + filePath

		// Try to open the file from embedded filesystem
		file, err := viewsFS.Open(fullPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()

		// Get file info
		fileInfo, err := file.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Set proper MIME type based on file extension
		contentType := mime.TypeByExtension(filepath.Ext(fileInfo.Name()))
		if contentType == "" {
			// Fallback MIME types for common file types
			switch strings.ToLower(filepath.Ext(fileInfo.Name())) {
			case ".css":
				contentType = "text/css"
			case ".js":
				contentType = "application/javascript"
			case ".svg":
				contentType = "image/svg+xml"
			case ".png":
				contentType = "image/png"
			case ".jpg", ".jpeg":
				contentType = "image/jpeg"
			case ".ico":
				contentType = "image/x-icon"
			default:
				contentType = "application/octet-stream"
			}
		}

		// Set headers
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=3600") // Cache for 1 hour

		// Copy file content to response
		_, err = io.Copy(w, file)
		if err != nil {
			http.Error(w, "Error serving file", http.StatusInternalServerError)
			return
		}
	}))

	http.Handle("/static/", staticHandler)

	http.HandleFunc("/", handleLogin)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/dashboard", requireAuth(handleDashboard))
	http.HandleFunc("/backup", requireAuth(handleBackup))
	http.HandleFunc("/settings", requireAuth(handleSettings))
	http.HandleFunc("/logout", handleLogout)

	http.HandleFunc("/api/settings/load", requireAuth(handleLoadSettings))
	http.HandleFunc("/api/settings/save", requireAuth(handleSaveSettings))
	http.HandleFunc("/api/settings/reset", requireAuth(handleResetSettings))
	http.HandleFunc("/api/schedule/info", requireAuth(handleScheduleInfo))
	http.HandleFunc("/api/schedule/status", requireAuth(handleScheduleStatus))
	http.HandleFunc("/api/test-connection", requireAuth(handleTestConnection))
	http.HandleFunc("/api/validate-binary", requireAuth(handleValidateBinary))
	http.HandleFunc("/api/detect-binary", requireAuth(handleDetectBinary))
	http.HandleFunc("/api/test-results", requireAuth(handleGetTestResults))
	http.HandleFunc("/api/system-metrics", requireAuth(handleGetSystemMetrics))
	http.HandleFunc("/api/databases", requireValidTests(requireAuth(handleGetDatabases)))
	http.HandleFunc("/api/backup/start", requireValidTests(requireAuth(handleStartBackup)))
	http.HandleFunc("/api/backup/stop", requireAuth(handleStopBackups))
	http.HandleFunc("/api/backup/running", requireAuth(handleGetRunningJobs))
	http.HandleFunc("/api/backup/recent-activity", requireAuth(handleGetRecentActivity))
	http.HandleFunc("/api/backup/jobs", requireAuth(handleGetBackupJobs))
	http.HandleFunc("/api/backup/history", requireAuth(handleGetBackupHistory))
	http.HandleFunc("/api/backup/database-groups/", requireAuth(handleGetDatabaseBackupGroups))
	http.HandleFunc("/api/backup/download/", requireAuth(handleDownloadBackup))
	http.HandleFunc("/api/backup/download-file", requireAuth(handleDownloadBackupFile))
	http.HandleFunc("/api/backup/download-group-zip", requireAuth(handleDownloadBackupGroupZip))
	http.HandleFunc("/api/backup/delete-group", requireAuth(handleDeleteBackupGroup))
	http.HandleFunc("/api/logging/status", requireAuth(handleLoggingStatus))
	http.HandleFunc("/api/logs/stream", requireAuth(handleLogStream))
	http.HandleFunc("/api/logs/delete", requireAuth(handleDeleteLogFile))
	http.HandleFunc("/api/logs/debug", requireAuth(handleLogDebug))
	http.HandleFunc("/api/backup/history/clear", requireAuth(handleClearBackupHistory))
	http.HandleFunc("/api/backup/timeline", requireAuth(handleGetBackupTimeline))
	http.HandleFunc("/ws/jobs", requireAuth(handleJobsWebSocket))
	http.HandleFunc("/ws/system", requireAuth(handleSystemWebSocket))
	http.HandleFunc("/ws/logs", requireAuth(handleLogsWebSocket))

	LogInfo("Routes configured successfully")
}

func requireAuth(handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		sessionsMutex.RLock()
		sessionTime, exists := sessions[cookie.Value]
		sessionsMutex.RUnlock()

		if !exists || time.Since(sessionTime) > sessionTimeout {
			sessionsMutex.Lock()
			delete(sessions, cookie.Value)
			sessionsMutex.Unlock()
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		sessionsMutex.Lock()
		sessions[cookie.Value] = time.Now()
		sessionsMutex.Unlock()
		handler(w, r)
	}
}

func requireValidTests(handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !testState.ButtonsEnabled {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Database connection or binary validation failed. Please check your configuration.",
				"details": map[string]interface{}{
					"connection_status":  testState.ConnectionStatus,
					"connection_message": testState.ConnectionMessage,
					"binary_status":      testState.BinaryStatus,
					"binary_message":     testState.BinaryMessage,
				},
			})
			return
		}

		handler(w, r)
	}
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		renderTemplate(w, "login.html", map[string]interface{}{
			"Title":   "Login - MariaDB Backup Tool",
			"Version": Version,
		})
		return
	}

	if r.Method == "POST" {
		username := r.FormValue("username")
		password := r.FormValue("password")

		// Load current config
		config, err := loadConfig("config.json")
		if err != nil {
			http.Error(w, "Failed to load config", http.StatusInternalServerError)
			return
		}

		// Validate credentials
		if username == config.Web.AuthUser && checkPassword(password, config.Web.AuthPassHash) {
			// Create session
			sessionID := generateSessionID()
			sessionsMutex.Lock()
			sessions[sessionID] = time.Now()
			sessionsMutex.Unlock()

			// Set session cookie
			cookie := &http.Cookie{
				Name:     "session_id",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				MaxAge:   int(sessionTimeout.Seconds()),
			}
			http.SetCookie(w, cookie)

			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}

		// Invalid credentials
		renderTemplate(w, "login.html", map[string]interface{}{
			"Title":   "Login - MariaDB Backup Tool",
			"Error":   "Invalid username or password",
			"Version": Version,
		})
	}
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "dashboard.html", map[string]interface{}{
		"Title": "Dashboard - MariaDB Backup Tool",
	})
}

func handleBackup(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "backup.html", map[string]interface{}{
		"Title": "Backup - MariaDB Backup Tool",
	})
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Load current config
		config, err := loadConfig("config.json")
		if err != nil {
			http.Error(w, "Failed to load config", http.StatusInternalServerError)
			return
		}

		// Get absolute path of config file
		configPath, err := filepath.Abs("config.json")
		if err != nil {
			configPath = "config.json" // fallback to relative path
		}

		renderTemplate(w, "settings.html", map[string]interface{}{
			"Title":      "Settings - MariaDB Backup Tool",
			"Config":     config,
			"ConfigPath": configPath,
		})
		return
	}

	if r.Method == "POST" {
		// Handle settings save
		http.Redirect(w, r, "/settings?saved=true", http.StatusFound)
	}
}

func handleLoadSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	config, err := loadConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load config: " + err.Error(),
		})
		return
	}

	// Include test results in the response
	response := map[string]interface{}{
		"success": true,
		"config":  config,
		"test_results": map[string]interface{}{
			"connection_status":  testState.ConnectionStatus,
			"connection_message": testState.ConnectionMessage,
			"binary_status":      testState.BinaryStatus,
			"binary_message":     testState.BinaryMessage,
			"binlog_format":      testState.BinlogFormat,
			"binlog_path":        testState.BinlogPath,
			"last_tested":        testState.LastTested.Format("2006-01-02 15:04:05"),
			"buttons_enabled":    testState.ButtonsEnabled,
		},
	}

	json.NewEncoder(w).Encode(response)
}

// handleScheduleInfo API endpoint to get backup schedule information
func handleScheduleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config, err := loadConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load config: " + err.Error(),
		})
		return
	}

	// Get last backup time from database
	lastBackupTime := getLastBackupTime()

	// Calculate next backup time
	nextBackupTime := calculateNextBackupTime(config.Backup.BackupStartTime, config.Backup.BackupIntervalHours)

	// Check if scheduler is disabled
	isSchedulerDisabled := config.Backup.BackupIntervalHours == 0
	if isSchedulerDisabled {
		nextBackupTime = "Disabled"
	}

	scheduleInfo := map[string]interface{}{
		"interval_hours":       config.Backup.BackupIntervalHours,
		"start_time":           config.Backup.BackupStartTime,
		"default_mode":         config.Backup.DefaultBackupMode,
		"last_backup_time":     lastBackupTime,
		"next_backup_time":     nextBackupTime,
		"full_backup_interval": config.Backup.FullBackupInterval,
		"is_disabled":          isSchedulerDisabled,
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"schedule": scheduleInfo,
	})
}

// handleScheduleStatus API endpoint to get scheduler status
func handleScheduleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := GetSchedulerStatus()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  status,
	})
}

// handleSaveSettings API endpoint to save settings
func handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	var config Config
	config.Database.Host = r.FormValue("db_host")
	config.Database.Port, _ = strconv.Atoi(r.FormValue("db_port"))
	config.Database.Username = r.FormValue("db_username")
	config.Database.Password = r.FormValue("db_password")
	config.Database.Socket = r.FormValue("db_socket")
	// binlog_path is now read-only and populated from database, not from form
	config.Database.BinaryDump = r.FormValue("binary_dump")
	config.Database.BinaryCheck = r.FormValue("binary_check")
	config.Database.BinaryBinLog = r.FormValue("binary_binlog")

	config.Backup.BackupDir = r.FormValue("backup_dir")
	config.Backup.RetentionBackups, _ = strconv.Atoi(r.FormValue("retention_backups"))
	config.Backup.Parallel, _ = strconv.Atoi(r.FormValue("parallel"))
	config.Backup.FullBackupInterval, _ = strconv.Atoi(r.FormValue("full_backup_interval"))
	config.Backup.BackupIntervalHours, _ = strconv.Atoi(r.FormValue("backup_interval_hours"))
	config.Backup.BackupStartTime = r.FormValue("backup_start_time")
	config.Backup.CompressionLevel, _ = strconv.Atoi(r.FormValue("compression_level"))
	config.Backup.NiceLevel, _ = strconv.Atoi(r.FormValue("nice_level"))
	config.Backup.DefaultBackupMode = r.FormValue("default_backup_mode")
	config.Backup.OptimizeTables = r.FormValue("optimize_tables") == "on"
	config.Backup.MaxMemoryThreshold, _ = strconv.Atoi(r.FormValue("max_memory_threshold"))
	config.Backup.MaxMemoryPerProcess = r.FormValue("max_memory_per_process")
	config.Backup.CreateTableInfo = r.FormValue("create_table_info") == "on"
	config.Backup.MysqldumpOptions = r.FormValue("mysqldump_options")
	config.Backup.MariadbCheckOptions = r.FormValue("mariadb_check_options")
	config.Backup.MariadbBinlogOptions = r.FormValue("mariadb_binlog_options")

	// Parse ignore databases
	ignoreDbsStr := r.FormValue("ignore_dbs")
	if ignoreDbsStr != "" {
		config.Backup.IgnoreDbs = strings.Split(ignoreDbsStr, "\n")
		// Clean up empty strings
		var cleaned []string
		for _, db := range config.Backup.IgnoreDbs {
			if strings.TrimSpace(db) != "" {
				cleaned = append(cleaned, strings.TrimSpace(db))
			}
		}
		config.Backup.IgnoreDbs = cleaned
	}

	config.Web.Port, _ = strconv.Atoi(r.FormValue("web_port"))
	config.Web.AuthUser = r.FormValue("auth_user")
	config.Web.SSLEnabled = r.FormValue("ssl_enabled") == "on"
	config.Web.SSLCertFile = r.FormValue("ssl_cert_file")
	config.Web.SSLKeyFile = r.FormValue("ssl_key_file")

	// Handle password change
	newPassword := r.FormValue("new_password")
	if newPassword != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Failed to hash password: " + err.Error(),
			})
			return
		}
		config.Web.AuthPassHash = string(hashedPassword)
	} else {
		// Keep existing password hash
		existingConfig, _ := loadConfig("config.json")
		if existingConfig != nil {
			config.Web.AuthPassHash = existingConfig.Web.AuthPassHash
		}
	}

	config.Logging.LogDir = r.FormValue("log_dir")
	config.Logging.RetentionLogs, _ = strconv.Atoi(r.FormValue("log_retention_days"))

	config.Notification.SlackWebhookURL = r.FormValue("slack_webhook")

	// Save config
	if err := saveConfig(&config, "config.json"); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to save config: " + err.Error(),
		})
		return
	}

	// Reload scheduler with new configuration
	ReloadSchedulerConfig(&config)
	LogInfo("Settings saved and scheduler configuration reloaded")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Settings saved successfully",
	})
}

// handleResetSettings resets the configuration to default
func handleResetSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Create default config
	defaultConfig, err := createDefaultConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to create default config: " + err.Error(),
		})
		return
	}

	// Save default config
	if err := saveConfig(defaultConfig, "config.json"); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to reset config: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Settings reset to default successfully",
	})
}

// handleLogout handles logout
func handleLogout(w http.ResponseWriter, r *http.Request) {
	// Get session cookie and remove from sessions map
	if cookie, err := r.Cookie("session_id"); err == nil {
		sessionsMutex.Lock()
		delete(sessions, cookie.Value)
		sessionsMutex.Unlock()
	}

	// Clear session cookie
	cookie := &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	}
	http.SetCookie(w, cookie)

	http.Redirect(w, r, "/login", http.StatusFound)
}

// renderTemplate renders HTML template
func renderTemplate(w http.ResponseWriter, tmpl string, data interface{}) {
	// Use a buffer to render the template first
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, tmpl, data); err != nil {
		LogError("Template rendering error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Write the buffered content to the response
	w.Write(buf.Bytes())
}

// checkPassword verifies password against hash
func checkPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// generateSessionID generates a random session ID
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// handleTestConnection API endpoint to test MySQL connection
func handleTestConnection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Load current config
	config, err := loadConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load config: " + err.Error(),
		})
		return
	}

	// Test MySQL connection
	result := testMySQLConnection(config)

	// Save test results to memory
	testState.ConnectionStatus = result["status"].(string)
	testState.ConnectionMessage = result["message"].(string)
	testState.LastTested = time.Now()

	// Update button state based on connection test
	if result["status"] == "success" || result["status"] == "warning" {
		// Connection successful, but buttons will be enabled only after binary validation
		// Don't enable buttons here, wait for binary validation
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": result["message"],
			"details": result["details"],
		})
	} else {
		// Connection failed, disable buttons
		testState.ButtonsEnabled = false
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   result["message"],
		})
	}
}

// buildMySQLDSN builds MySQL connection string based on config
func buildMySQLDSN(config *Config) (string, error) {
	if config.Database.Port > 0 && config.Database.Host != "" {
		// Use TCP connection when port is specified and host is not empty
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/",
			config.Database.Username,
			config.Database.Password,
			config.Database.Host,
			config.Database.Port), nil
	} else if config.Database.Socket != "" {
		// Use Unix socket when port is 0/blank or host is empty, and socket is specified
		return fmt.Sprintf("%s:%s@unix(%s)/",
			config.Database.Username,
			config.Database.Password,
			config.Database.Socket), nil
	} else {
		return "", fmt.Errorf("invalid database configuration: either host:port or socket must be specified")
	}
}

// buildMySQLConnectionArgs builds MySQL connection arguments for mysqldump/mysql commands
func buildMySQLConnectionArgs(config *Config) []string {
	var args []string

	if config.Database.Port > 0 && config.Database.Host != "" {
		// Use TCP connection
		args = append(args, "-h", config.Database.Host)
		args = append(args, "-P", fmt.Sprintf("%d", config.Database.Port))
	} else if config.Database.Socket != "" {
		// Use Unix socket
		args = append(args, "-S", config.Database.Socket)
	}

	// Add username
	if config.Database.Username != "" {
		args = append(args, "-u", config.Database.Username)
	}

	// Add password if specified
	if config.Database.Password != "" {
		args = append(args, "-p"+config.Database.Password)
	}

	return args
}

// handleValidateBinary API endpoint to validate binary files and binlog settings
func handleValidateBinary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Load current config
	config, err := loadConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load config: " + err.Error(),
		})
		return
	}

	// Validate binary configuration
	result := validateBinaryConfiguration(config)

	// Save test results to memory
	testState.BinaryStatus = result["status"].(string)
	testState.BinaryMessage = result["message"].(string)
	if binlogFormat, exists := result["binlog_format"]; exists {
		testState.BinlogFormat = binlogFormat.(string)
	}
	if binlogPath, exists := result["binlog_path"]; exists {
		testState.BinlogPath = binlogPath.(string)
	}
	testState.LastTested = time.Now()

	// Update button state based on binary validation
	if result["status"] == "success" || result["status"] == "warning" {
		// Binary validation successful or with warnings, enable buttons
		testState.ButtonsEnabled = true
	} else {
		// Binary validation failed, disable buttons
		testState.ButtonsEnabled = false
	}

	// Create response based on status
	var response map[string]interface{}
	if result["status"] == "success" {
		response = map[string]interface{}{
			"success": true,
			"message": result["message"],
			"details": result["details"],
		}
	} else {
		response = map[string]interface{}{
			"success": false,
			"error":   result["details"],
			"message": result["message"],
		}
	}

	// Add binlog format if available
	if binlogFormat, exists := result["binlog_format"]; exists {
		response["binlog_format"] = binlogFormat
	}

	// Add binlog path if available
	if binlogPath, exists := result["binlog_path"]; exists {
		response["binlog_path"] = binlogPath
	}

	json.NewEncoder(w).Encode(response)
}

// handleGetTestResults returns stored test results
func handleGetTestResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Return in-memory test state
	results := map[string]interface{}{
		"connection_status":  testState.ConnectionStatus,
		"connection_message": testState.ConnectionMessage,
		"binary_status":      testState.BinaryStatus,
		"binary_message":     testState.BinaryMessage,
		"binlog_format":      testState.BinlogFormat,
		"binlog_path":        testState.BinlogPath,
		"last_tested":        testState.LastTested.Format("2006-01-02 15:04:05"),
		"buttons_enabled":    testState.ButtonsEnabled,
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"results": results,
	})
}

// handleGetSystemMetrics returns current system metrics
func handleGetSystemMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := getSystemMetrics()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"metrics": metrics,
	})
}

// handleGetDatabases returns list of available databases
func handleGetDatabases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config, err := loadConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load config: " + err.Error(),
		})
		return
	}

	databases, err := getDatabases(config)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to get databases: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"databases": databases,
	})
}

// getDatabases queries MySQL for available databases
func getDatabases(config *Config) ([]string, error) {
	// Build connection string
	dsn, err := buildMySQLDSN(config)
	if err != nil {
		return nil, err
	}

	// Connect to MySQL
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, err
	}

	// Query for databases
	rows, err := db.Query("SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			continue
		}

		// Filter out system databases
		if dbName == "information_schema" ||
			dbName == "performance_schema" ||
			dbName == "mysql" ||
			dbName == "sys" {
			continue
		}

		// Filter out ignored databases from config
		shouldIgnore := false
		for _, ignoredDB := range config.Backup.IgnoreDbs {
			if dbName == ignoredDB {
				shouldIgnore = true
				break
			}
		}

		if !shouldIgnore {
			databases = append(databases, dbName)
		}
	}

	return databases, nil
}

// getSystemMetrics gets current CPU and memory usage
func getSystemMetrics() map[string]interface{} {
	// Get CPU usage
	cpuPercent, _ := getCPUUsage()

	// Get CPU count, core count, and thread count
	cpuCount, cpuCores, cpuThreads, _ := getCPUCoreCount()

	// Get memory usage
	memPercent, memUsed, memTotal, err := getMemoryUsage()
	if err != nil {
		memPercent = 0
		memUsed = 0
		memTotal = 0
	}

	// Get disk usage
	diskPercent, diskUsed, diskTotal, err := getDiskUsage()
	if err != nil {
		diskPercent = 0
		diskUsed = 0
		diskTotal = 0
	}

	return map[string]interface{}{
		"cpu_percent":    cpuPercent,
		"cpu_count":      cpuCount,
		"cpu_cores":      cpuCores,
		"cpu_threads":    cpuThreads,
		"memory_percent": memPercent,
		"memory_used":    memUsed,
		"memory_total":   memTotal,
		"disk_percent":   diskPercent,
		"disk_used":      diskUsed,
		"disk_total":     diskTotal,
		"timestamp":      time.Now().Unix(),
	}
}

// getCPUUsage gets current CPU usage percentage
func getCPUUsage() (float64, error) {
	// Get CPU usage over 1 second interval for accuracy
	percentages, err := cpu.Percent(time.Second, false)
	if err != nil {
		return 0, err
	}

	if len(percentages) == 0 {
		return 0, fmt.Errorf("no CPU data available")
	}

	// Return the average CPU usage
	return percentages[0], nil
}

// getCPUCoreCount gets the number of CPU sockets, physical cores, and logical threads
func getCPUCoreCount() (int, int, int, error) {
	// Get CPU info for physical CPU count
	cpuInfo, err := cpu.Info()
	if err != nil {
		return 0, 0, 0, err
	}

	if len(cpuInfo) == 0 {
		return 0, 0, 0, fmt.Errorf("no CPU info available")
	}

	// Count unique physical CPU sockets using PhysicalID
	physicalCPUs := make(map[string]bool)
	for _, ci := range cpuInfo {
		if ci.PhysicalID != "" {
			physicalCPUs[ci.PhysicalID] = true
		}
	}
	cpuCount := len(physicalCPUs)

	// If PhysicalID is empty (some systems), fall back to counting CPU entries
	if cpuCount == 0 {
		cpuCount = len(cpuInfo)
	}

	// Get physical cores and logical threads using cpu.Counts()
	physicalCores, err := cpu.Counts(false) // false = physical cores only
	if err != nil {
		return 0, 0, 0, err
	}

	logicalThreads, err := cpu.Counts(true) // true = logical threads (including hyperthreading)
	if err != nil {
		return 0, 0, 0, err
	}

	return cpuCount, physicalCores, logicalThreads, nil
}

// getMemoryUsage gets current memory usage
func getMemoryUsage() (float64, uint64, uint64, error) {
	// Get virtual memory statistics
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, 0, err
	}

	// Calculate percentage
	percent := vmStat.UsedPercent

	// Convert to GB
	usedGB := vmStat.Used / 1024 / 1024 / 1024
	totalGB := vmStat.Total / 1024 / 1024 / 1024

	return percent, usedGB, totalGB, nil
}

// getDiskUsage gets current disk usage for the backup directory
func getDiskUsage() (float64, uint64, uint64, error) {
	// Load config to get backup directory
	config, err := loadConfig("config.json")
	if err != nil {
		return 0, 0, 0, err
	}

	// Get backup directory from config
	backupDir := config.Backup.BackupDir
	if backupDir == "" {
		// Fallback to current directory if backup directory not set
		backupDir = "."
	}

	// Get the root of the filesystem that contains the backup directory
	// This ensures we get the disk usage of the actual disk/partition, not just the directory
	rootPath, err := getFilesystemRoot(backupDir)
	if err != nil {
		return 0, 0, 0, err
	}

	// Get disk usage statistics for the root of the filesystem
	diskStat, err := disk.Usage(rootPath)
	if err != nil {
		return 0, 0, 0, err
	}

	// Calculate percentage
	percent := diskStat.UsedPercent

	// Convert to GB
	usedGB := diskStat.Used / 1024 / 1024 / 1024
	totalGB := diskStat.Total / 1024 / 1024 / 1024

	return percent, usedGB, totalGB, nil
}

// getFilesystemRoot finds the root of the filesystem containing the given path
func getFilesystemRoot(path string) (string, error) {
	// Convert to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// For Windows, extract the drive letter (e.g., D: from D:/test)
	if filepath.VolumeName(absPath) != "" {
		// Return the drive root (e.g., D:\)
		return filepath.VolumeName(absPath) + string(filepath.Separator), nil
	}

	// For Unix-like systems, walk up the directory tree to find the mount point
	current := absPath
	for {
		parent := filepath.Dir(current)
		if parent == current {
			// We've reached the root
			break
		}

		if parent == "/" {
			_, err := disk.Usage(current)
			if err == nil {
				break
			}
		}

		current = parent
	}

	return current, nil
}

// validateBinaryConfiguration validates binary files and binlog settings
func validateBinaryConfiguration(config *Config) map[string]interface{} {
	result := map[string]interface{}{
		"status":  "unknown",
		"message": "Not validated",
		"details": "",
	}

	var errors []string
	var warnings []string
	var successes []string

	binaries := map[string]string{
		"mysqldump":   config.Database.BinaryDump,
		"mysqlcheck":  config.Database.BinaryCheck,
		"mysqlbinlog": config.Database.BinaryBinLog,
	}

	for name, path := range binaries {
		if path == "" {
			errors = append(errors, fmt.Sprintf("%s path not configured", name))
			continue
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("%s file not found at: %s", name, path))
			continue
		}
		successes = append(successes, fmt.Sprintf("%s found at: %s", name, path))
	}

	binlogResult := validateBinlogSettings(config)
	if binlogFormat, exists := binlogResult["binlog_format"]; exists {
		result["binlog_format"] = binlogFormat
	}
	if binlogPath, exists := binlogResult["binlog_path"]; exists {
		result["binlog_path"] = binlogPath
	}
	if binlogResult["status"] == "success" {
		successes = append(successes, binlogResult["message"].(string))
	} else if binlogResult["status"] == "warning" {
		warnings = append(warnings, binlogResult["message"].(string))
	} else {
		errors = append(errors, binlogResult["message"].(string))
	}

	if len(errors) > 0 {
		result["status"] = "failed"
		result["message"] = "Binary validation failed"
		var detailsParts []string
		detailsParts = append(detailsParts, "❌ Errors:")
		for _, err := range errors {
			detailsParts = append(detailsParts, "  • "+err)
		}
		if len(warnings) > 0 {
			detailsParts = append(detailsParts, "⚠️ Warnings:")
			for _, warn := range warnings {
				detailsParts = append(detailsParts, "  • "+warn)
			}
		}
		if len(successes) > 0 {
			detailsParts = append(detailsParts, "✅ Successes:")
			for _, success := range successes {
				detailsParts = append(detailsParts, "  • "+success)
			}
		}
		result["details"] = strings.Join(detailsParts, "\n")
	} else if len(warnings) > 0 {
		result["status"] = "warning"
		result["message"] = "Binary validation completed with warnings"
		var detailsParts []string
		detailsParts = append(detailsParts, "⚠️ Warnings:")
		for _, warn := range warnings {
			detailsParts = append(detailsParts, "  • "+warn)
		}
		if len(successes) > 0 {
			detailsParts = append(detailsParts, "✅ Successes:")
			for _, success := range successes {
				detailsParts = append(detailsParts, "  • "+success)
			}
		}
		result["details"] = strings.Join(detailsParts, "\n")
	} else {
		result["status"] = "success"
		result["message"] = "Binary validation successful"
		var detailsParts []string
		detailsParts = append(detailsParts, "✅ Successes:")
		for _, success := range successes {
			detailsParts = append(detailsParts, "  • "+success)
		}
		result["details"] = strings.Join(detailsParts, "\n")
	}

	return result
}

// validateBinlogSettings validates binlog path and format via MySQL connection
func validateBinlogSettings(config *Config) map[string]interface{} {
	result := map[string]interface{}{
		"status":  "unknown",
		"message": "Not validated",
		"details": "",
	}

	dsn, err := buildMySQLDSN(config)
	if err != nil {
		result["status"] = "failed"
		result["message"] = "Cannot connect to MySQL: " + err.Error()
		return result
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		result["status"] = "failed"
		result["message"] = "Failed to connect to MySQL: " + err.Error()
		return result
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		result["status"] = "failed"
		result["message"] = "MySQL connection failed: " + err.Error()
		return result
	}

	var binlogFormat string
	err = db.QueryRow("SHOW VARIABLES LIKE 'binlog_format'").Scan(&binlogFormat, &binlogFormat)
	if err != nil {
		result["status"] = "failed"
		result["message"] = "Failed to query binlog_format variable: " + err.Error()
		return result
	}
	result["binlog_format"] = binlogFormat

	// Get binlog basename from database
	var binlogBasename string
	err = db.QueryRow("SHOW VARIABLES LIKE 'log_bin_basename'").Scan(&binlogBasename, &binlogBasename)
	if err != nil {
		result["status"] = "failed"
		result["message"] = "Failed to query log_bin_basename variable: " + err.Error()
		return result
	}
	result["binlog_path"] = binlogBasename + ".[0-9]*"

	var logBin string
	err = db.QueryRow("SHOW VARIABLES LIKE 'log_bin'").Scan(&logBin, &logBin)
	if err != nil {
		result["status"] = "failed"
		result["message"] = "Failed to query log_bin variable: " + err.Error()
		return result
	}

	if logBin != "ON" {
		result["status"] = "failed"
		result["message"] = "Binary logging is not enabled on this MySQL server"
		return result
	}

	binlogPath := binlogBasename + ".*"

	matches, err := filepath.Glob(binlogPath)
	if err != nil {
		result["status"] = "failed"
		result["message"] = "Invalid binlog path pattern: " + err.Error()
		return result
	}

	if len(matches) == 0 {
		result["status"] = "warning"
		result["message"] = "No binlog files found matching pattern"
		result["details"] = "Pattern: " + binlogPath
		return result
	}

	if binlogFormat == "STATEMENT" {
		result["status"] = "success"
		result["message"] = "Binlog format is optimal (STATEMENT)"
		result["details"] = fmt.Sprintf("✅ Format: %s (optimal for incremental backups)\n✅ Found %d binlog files\n✅ Pattern: %s", binlogFormat, len(matches), binlogPath)
	} else {
		result["status"] = "warning"
		result["message"] = fmt.Sprintf("Binlog format is %s (STATEMENT recommended)", binlogFormat)
		result["details"] = fmt.Sprintf("⚠️ Format: %s (STATEMENT recommended for incremental backups)\n✅ Found %d binlog files\n✅ Pattern: %s", binlogFormat, len(matches), binlogPath)
	}

	return result
}

func autoTestConnectionsOnStart(config *Config) {
	connResult := testMySQLConnection(config)
	connStatus := connResult["status"].(string)
	connMessage := connResult["message"].(string)

	if connStatus == "success" || connStatus == "warning" {
		LogInfo("✅ MySQL connection test successful")

		// If connection succeeds, test binary configuration
		LogDebug("Testing binary configuration...")
		binaryResult := validateBinaryConfiguration(config)
		binaryStatus := binaryResult["status"].(string)
		binaryMessage := binaryResult["message"].(string)
		binlogFormat := ""
		if format, exists := binaryResult["binlog_format"]; exists {
			binlogFormat = format.(string)
		}
		binlogPath := ""
		if path, exists := binaryResult["binlog_path"]; exists {
			binlogPath = path.(string)
		}

		if binaryStatus == "success" {
			LogInfo("✅ Binary configuration validation successful")
			// Enable buttons only if both connection and binary validation succeed
			testState.ButtonsEnabled = true
		} else if binaryStatus == "warning" {
			LogWarn("⚠️ Binary configuration validation completed with warnings: %s", binaryMessage)
			// Enable buttons even with warnings
			testState.ButtonsEnabled = true
		} else {
			LogError("❌ Binary configuration validation failed: %s", binaryMessage)
			// Disable buttons if binary validation fails
			testState.ButtonsEnabled = false
		}

		// Save test results to memory
		testState.ConnectionStatus = connStatus
		testState.ConnectionMessage = connMessage
		testState.BinaryStatus = binaryStatus
		testState.BinaryMessage = binaryMessage
		testState.BinlogFormat = binlogFormat
		testState.BinlogPath = binlogPath
		testState.LastTested = time.Now()
	} else {
		LogError("❌ MySQL connection test failed: %s", connMessage)
		// Save connection failure results and disable buttons
		testState.ConnectionStatus = connStatus
		testState.ConnectionMessage = connMessage
		testState.BinaryStatus = "unknown"
		testState.BinaryMessage = ""
		testState.BinlogFormat = ""
		testState.BinlogPath = ""
		testState.ButtonsEnabled = false
		testState.LastTested = time.Now()
	}

	LogInfo("Auto-testing completed")
}

// testMySQLConnection tests MySQL database connection
func testMySQLConnection(config *Config) map[string]interface{} {
	result := map[string]interface{}{
		"status":  "unknown",
		"message": "Not tested",
		"details": "",
	}

	// Build connection string
	dsn, err := buildMySQLDSN(config)
	if err != nil {
		result["status"] = "failed"
		result["message"] = err.Error()
		return result
	}

	// Connect to MySQL
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		result["status"] = "failed"
		result["message"] = "Failed to connect to MySQL: " + err.Error()
		return result
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		result["status"] = "failed"
		result["message"] = "MySQL connection failed: " + err.Error()
		return result
	}

	// Test version query
	var version string
	err = db.QueryRow("SELECT VERSION()").Scan(&version)
	if err != nil {
		result["status"] = "warning"
		result["message"] = "Connected but version query failed: " + err.Error()
		result["details"] = "Connection successful but unable to verify MySQL version"
		return result
	}

	result["status"] = "success"
	if config.Database.Port > 0 && config.Database.Host != "" {
		result["message"] = "MySQL connection successful (TCP)"
		result["details"] = fmt.Sprintf("Connected via TCP to %s:%d - MySQL version: %s", config.Database.Host, config.Database.Port, version)
	} else {
		result["message"] = "MySQL connection successful (Unix Socket)"
		result["details"] = fmt.Sprintf("Connected via Unix socket %s - MySQL version: %s", config.Database.Socket, version)
	}
	return result
}

// handleGetRunningJobs returns running backup jobs
func handleGetRunningJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	runningData, err := GetRunningJobsWithSummary()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch jobs: " + err.Error(),
			"data":    map[string]interface{}{},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    runningData,
	})
}

// handleGetBackupJobs returns all backup jobs (alias for running jobs)
func handleGetBackupJobs(w http.ResponseWriter, r *http.Request) {
	handleGetRunningJobs(w, r)
}

// handleGetRecentActivity returns paginated recent activity for dashboard
func handleGetRecentActivity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	// Set defaults
	page := 1
	limit := 5

	// Parse page parameter
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	// Parse limit parameter
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 50 {
			limit = l
		}
	}

	// Get paginated recent activity data
	activityData, err := GetRecentActivityWithPagination(page, limit)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch recent activity: " + err.Error(),
			"data":    map[string]interface{}{},
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    activityData,
	})
}

// handleGetBackupHistory returns paginated backup history with search
func handleGetBackupHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")
	search := r.URL.Query().Get("search")
	status := r.URL.Query().Get("status")
	date := r.URL.Query().Get("date")
	sort := r.URL.Query().Get("sort")
	jobId := r.URL.Query().Get("job_id")

	// Set defaults
	page := 1
	limit := 20
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	// Get backup history from database
	history, totalCount, err := GetBackupHistory(page, limit, search, status, date, sort, jobId)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch backup history: " + err.Error(),
		})
		return
	}

	// Calculate pagination info
	totalPages := 1 // Default to 1 page
	if totalCount > 0 {
		totalPages = (totalCount + limit - 1) / limit
	}
	hasNext := page < totalPages
	hasPrev := page > 1

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    history,
		"pagination": map[string]interface{}{
			"current_page": page,
			"total_pages":  totalPages,
			"total_count":  totalCount,
			"limit":        limit,
			"has_next":     hasNext,
			"has_prev":     hasPrev,
		},
	})
}

// handleGetDatabaseBackupGroups returns backup groups for a specific database
func handleGetDatabaseBackupGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract database name from URL path
	// URL format: /api/backup/database-groups/{database_name}
	path := r.URL.Path
	pathParts := strings.Split(path, "/")
	if len(pathParts) < 5 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Database name not provided in URL",
		})
		return
	}

	databaseName := pathParts[4]
	if databaseName == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Database name cannot be empty",
		})
		return
	}

	// Get pagination parameters
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	page := 1
	limit := 10 // Default to 10 groups per page

	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 50 {
			limit = l
		}
	}

	LogInfo("Database backup files request for: %s (page %d, limit %d)", databaseName, page, limit)

	// Load current config
	config, err := loadConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load config: " + err.Error(),
		})
		return
	}

	// Get backup files for the database from filesystem with pagination
	groups, totalGroups, err := GetDatabaseBackupFiles(databaseName, config, page, limit)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch backup groups: " + err.Error(),
		})
		return
	}

	// Calculate pagination info
	totalPages := 1
	if totalGroups > 0 {
		totalPages = (totalGroups + limit - 1) / limit
	}
	hasNext := page < totalPages
	hasPrev := page > 1

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"database_name": databaseName,
		"groups":        groups,
		"total_groups":  totalGroups,
		"pagination": map[string]interface{}{
			"current_page": page,
			"total_pages":  totalPages,
			"limit":        limit,
			"has_next":     hasNext,
			"has_prev":     hasPrev,
		},
	})
}

// handleDownloadBackupFile handles downloading backup files directly from filesystem
func handleDownloadBackupFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get file path from query parameter
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "File path not provided", http.StatusBadRequest)
		return
	}

	// Load current config
	config, err := loadConfig("config.json")
	if err != nil {
		http.Error(w, "Failed to load config", http.StatusInternalServerError)
		return
	}

	// Security check: ensure the file is within the backup directory
	backupDir := config.Backup.BackupDir
	LogInfo("Checking file path: %s against backup dir: %s", filePath, backupDir)

	// Normalize paths for comparison
	normalizedFilePath := filepath.Clean(filePath)
	normalizedBackupDir := filepath.Clean(backupDir)

	if !strings.HasPrefix(normalizedFilePath, normalizedBackupDir) {
		LogWarn("Access denied: file %s is outside backup directory %s", normalizedFilePath, normalizedBackupDir)
		http.Error(w, "Access denied: file outside backup directory", http.StatusForbidden)
		return
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Get filename for download
	fileName := filepath.Base(filePath)

	// Set headers for file download
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	w.Header().Set("Content-Type", "application/octet-stream")

	// Serve the file
	http.ServeFile(w, r, filePath)
}

// handleDownloadBackupGroupZip handles creating and downloading a ZIP file containing a full backup and its incremental backups
func handleDownloadBackupGroupZip(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var requestData struct {
		FullBackupPath   string   `json:"full_backup_path"`
		FullBackupName   string   `json:"full_backup_name"`
		IncrementalPaths []string `json:"incremental_paths"`
		DatabaseName     string   `json:"database_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Load current config
	config, err := loadConfig("config.json")
	if err != nil {
		http.Error(w, "Failed to load config", http.StatusInternalServerError)
		return
	}

	// Security check: ensure all files are within the backup directory
	backupDir := config.Backup.BackupDir
	allPaths := append([]string{requestData.FullBackupPath}, requestData.IncrementalPaths...)

	LogInfo("Checking ZIP file paths for database %s", requestData.DatabaseName)

	for i, filePath := range allPaths {
		LogInfo("Checking file %d: %s", i+1, filePath)

		// Normalize paths for comparison
		normalizedFilePath := filepath.Clean(filePath)
		normalizedBackupDir := filepath.Clean(backupDir)

		if !strings.HasPrefix(normalizedFilePath, normalizedBackupDir) {
			LogWarn("Access denied: file %s is outside backup directory %s", normalizedFilePath, normalizedBackupDir)
			http.Error(w, "Access denied: file outside backup directory", http.StatusForbidden)
			return
		}
	}

	// Check if all files exist
	for _, filePath := range allPaths {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			http.Error(w, fmt.Sprintf("File not found: %s", filePath), http.StatusNotFound)
			return
		}
	}

	// Create ZIP file
	zipFileName := fmt.Sprintf("%s_backup_group_%s.zip", requestData.DatabaseName, time.Now().Format("20060102_150405"))

	// Set headers for ZIP download
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", zipFileName))
	w.Header().Set("Content-Type", "application/zip")

	// Create ZIP writer
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	// Add full backup file to ZIP
	if err := addFileToZip(zipWriter, requestData.FullBackupPath, requestData.FullBackupName); err != nil {
		LogError("Failed to add full backup to ZIP: %v", err)
		http.Error(w, "Failed to create ZIP file", http.StatusInternalServerError)
		return
	}

	// Add incremental backup files to ZIP
	for i, incPath := range requestData.IncrementalPaths {
		incFileName := filepath.Base(incPath)
		if err := addFileToZip(zipWriter, incPath, incFileName); err != nil {
			LogError("Failed to add incremental backup %d to ZIP: %v", i+1, err)
			http.Error(w, "Failed to create ZIP file", http.StatusInternalServerError)
			return
		}
	}

	LogInfo("Created ZIP file %s with %d files for database %s", zipFileName, len(allPaths), requestData.DatabaseName)
}

// handleDeleteBackupGroup handles deleting a full backup and all its incremental backups
func handleDeleteBackupGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var requestData struct {
		FullBackupPath   string   `json:"full_backup_path"`
		FullBackupName   string   `json:"full_backup_name"`
		IncrementalPaths []string `json:"incremental_paths"`
		DatabaseName     string   `json:"database_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid request body",
		})
		return
	}

	// Load current config
	config, err := loadConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load config",
		})
		return
	}

	// Security check: ensure all files are within the backup directory
	backupDir := config.Backup.BackupDir
	allPaths := append([]string{requestData.FullBackupPath}, requestData.IncrementalPaths...)

	LogInfo("Checking delete file paths for database %s", requestData.DatabaseName)

	for i, filePath := range allPaths {
		LogInfo("Checking file %d: %s", i+1, filePath)

		// Normalize paths for comparison
		normalizedFilePath := filepath.Clean(filePath)
		normalizedBackupDir := filepath.Clean(backupDir)

		if !strings.HasPrefix(normalizedFilePath, normalizedBackupDir) {
			LogWarn("Access denied: file %s is outside backup directory %s", normalizedFilePath, normalizedBackupDir)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Access denied: file outside backup directory",
			})
			return
		}
	}

	// Delete files
	deletedFiles := 0
	var errors []string
	var deletedFilePaths []string

	for _, filePath := range allPaths {
		if err := os.Remove(filePath); err != nil {
			LogError("Failed to delete file %s: %v", filePath, err)
			errors = append(errors, fmt.Sprintf("Failed to delete %s: %v", filepath.Base(filePath), err))
		} else {
			deletedFiles++
			deletedFilePaths = append(deletedFilePaths, filePath)
			LogInfo("Successfully deleted file: %s", filePath)
		}
	}

	if len(errors) > 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       false,
			"error":         "Some files could not be deleted",
			"deleted_files": deletedFiles,
			"errors":        errors,
		})
		return
	}

	LogInfo("Successfully deleted %d backup files for database %s", deletedFiles, requestData.DatabaseName)

	// Create deletion log for UI-initiated deletion
	if len(deletedFilePaths) > 0 {
		createDeletionLog(deletedFilePaths, "manual_deletion")
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"message":       fmt.Sprintf("Successfully deleted %d backup file(s)", deletedFiles),
		"deleted_files": deletedFiles,
	})
}

// addFileToZip adds a file to the ZIP archive
func addFileToZip(zipWriter *zip.Writer, filePath, fileName string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(fileInfo)
	if err != nil {
		return err
	}

	header.Name = fileName
	header.Method = zip.Deflate

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	return err
}

// handleStartBackup handles starting a new backup job
func handleStartBackup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Reset global abort flag when starting new backups
	ResetGlobalBackupAbort()

	// Parse request body
	var requestData struct {
		BackupMode string   `json:"backup_mode"`
		Databases  []string `json:"databases"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid request format: " + err.Error(),
		})
		return
	}

	// Validate request
	if len(requestData.Databases) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "No databases specified for backup",
		})
		return
	}

	if requestData.BackupMode == "" {
		requestData.BackupMode = "auto"
	}

	// Generate job ID using timestamp format
	jobID := GenerateJobID()

	LogInfo("Manual backup request received - JobID: %s, Mode: %s, Databases: %v",
		jobID, requestData.BackupMode, requestData.Databases)

	// Route to appropriate backup function based on mode
	switch requestData.BackupMode {
	case "full":
		// Call full backup function
		backupRequest := BackupFullRequest{
			JobID:       jobID,
			Databases:   requestData.Databases,
			BackupMode:  requestData.BackupMode,
			RequestedBy: "web_ui",
		}

		response := StartFullBackup(backupRequest)

		if response.Success {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": response.Message,
				"job_id":  response.JobID,
				"details": response.Details,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   response.Message,
				"job_id":  response.JobID,
				"details": response.Details,
			})
		}

	case "incremental":
		// Call incremental backup function
		backupRequest := BackupIncRequest{
			JobID:       jobID,
			Databases:   requestData.Databases,
			BackupMode:  requestData.BackupMode,
			RequestedBy: "web_ui",
		}

		response := StartIncBackup(backupRequest)

		if response.Success {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": response.Message,
				"job_id":  response.JobID,
				"details": response.Details,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   response.Message,
				"job_id":  response.JobID,
				"details": response.Details,
			})
		}

	case "auto":
		// Auto mode: determine full vs incremental for each database
		LogInfo("Auto mode selected - analyzing databases for full vs incremental backup")

		// Load config to get backup settings
		config, err := loadConfig("config.json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Failed to load configuration: " + err.Error(),
			})
			return
		}

		// Analyze each database to determine backup type
		fullBackupDBs := []string{}
		incBackupDBs := []string{}

		for _, dbName := range requestData.Databases {
			backupType := determineBackupType(dbName, config)
			if backupType == "full" {
				fullBackupDBs = append(fullBackupDBs, dbName)
			} else {
				incBackupDBs = append(incBackupDBs, dbName)
			}
		}

		LogInfo("Auto mode analysis - Full backups: %d databases, Incremental backups: %d databases", len(fullBackupDBs), len(incBackupDBs))

		// Start full backups if any databases need full backup
		if len(fullBackupDBs) > 0 {
			fullJobID := GenerateJobID()
			fullRequest := BackupFullRequest{
				JobID:       fullJobID,
				Databases:   fullBackupDBs,
				BackupMode:  "auto", // Use "auto" mode, backup-full.go will convert to "auto-full" type
				RequestedBy: "web_ui",
			}

			go StartFullBackup(fullRequest)
			LogInfo("Started full backup for %d databases (JobID: %s)", len(fullBackupDBs), fullJobID)
		}

		// Start incremental backups if any databases need incremental backup
		if len(incBackupDBs) > 0 {
			incJobID := GenerateJobID()
			incRequest := BackupIncRequest{
				JobID:       incJobID,
				Databases:   incBackupDBs,
				BackupMode:  "auto", // Use "auto" mode, backup-inc.go will convert to "auto-inc" type
				RequestedBy: "web_ui",
			}

			go StartIncBackup(incRequest)
			LogInfo("Started incremental backup for %d databases (JobID: %s)", len(incBackupDBs), incJobID)
		}

		// Return response
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Auto backup started - %d full, %d incremental", len(fullBackupDBs), len(incBackupDBs)),
			"job_id":  jobID,
			"details": map[string]interface{}{
				"backup_mode":  "auto",
				"databases":    requestData.Databases,
				"total_dbs":    len(requestData.Databases),
				"full_backups": fullBackupDBs,
				"inc_backups":  incBackupDBs,
				"full_count":   len(fullBackupDBs),
				"inc_count":    len(incBackupDBs),
			},
		})

	default:
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid backup mode: " + requestData.BackupMode,
		})
	}
}

// handleStopBackups handles stopping all running backup processes
func handleStopBackups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	LogInfo("Stop backup request received")

	// Signal global abort to stop all backup processes including queued ones
	SignalGlobalBackupAbort()

	// Get all active jobs (running, optimizing, etc.) from SQLite
	activeJobs, err := GetActiveJobs()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to get active jobs: " + err.Error(),
		})
		return
	}

	if len(activeJobs) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "No active backups to stop",
		})
		return
	}

	// Mark all active jobs as cancelled
	stoppedCount := 0
	cancelledJobIDs := make(map[string]bool) // Track unique job IDs

	for _, job := range activeJobs {
		jobID := job["job_id"].(string)
		databaseName := job["database_name"].(string)
		status := job["status"].(string)

		err := UpdateBackupJobStatus(jobID, "cancelled", "Stopped by user", false)
		if err != nil {
			LogError("Failed to cancel backup job %s for database %s: %v", jobID, databaseName, err)
		} else {
			stoppedCount++
			cancelledJobIDs[jobID] = true
			LogInfo("Cancelled backup job %s for database %s (was %s)", jobID, databaseName, status)
		}
	}

	// Cancel backup summaries for all affected job IDs
	for jobID := range cancelledJobIDs {
		err := CancelBackupSummary(jobID)
		if err != nil {
			LogError("Failed to cancel backup summary for job %s: %v", jobID, err)
		} else {
			LogInfo("Cancelled backup summary for job %s", jobID)
		}
	}

	LogInfo("Stopped %d active backup jobs, cancelled %d backup summaries, and signaled global abort", stoppedCount, len(cancelledJobIDs))

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"message":       fmt.Sprintf("Stopped %d active backup jobs", stoppedCount),
		"stopped_count": stoppedCount,
		"total_active":  len(activeJobs),
	})
}

// determineBackupType determines whether a database needs full or incremental backup
func determineBackupType(dbName string, config *Config) string {
	// Check if full backup exists and is within the full backup interval
	backupDir := filepath.Join(config.Backup.BackupDir, dbName)

	// Check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		//LogInfo("No backup directory found for %s - will do full backup", dbName)
		return "full"
	}

	// Look for full backup files in the directory
	fullBackupPattern := fmt.Sprintf("full_%s_*.sql", dbName)
	matches, err := filepath.Glob(filepath.Join(backupDir, fullBackupPattern))
	if err != nil {
		LogWarn("Error searching for full backup files for %s: %v - will do full backup", dbName, err)
		return "full"
	}

	// Also check for .gz files
	gzPattern := fmt.Sprintf("full_%s_*.gz", dbName)
	gzMatches, err := filepath.Glob(filepath.Join(backupDir, gzPattern))
	if err != nil {
		LogWarn("Error searching for compressed full backup files for %s: %v", dbName, err)
	} else {
		matches = append(matches, gzMatches...)
	}

	if len(matches) == 0 {
		LogInfo("No full backup found for %s - will do full backup", dbName)
		return "full"
	}

	// Find the most recent full backup
	var latestBackup string
	var latestTime time.Time

	for _, match := range matches {
		fileName := filepath.Base(match)
		nameWithoutExt := strings.TrimSuffix(fileName, ".sql")
		nameWithoutExt = strings.TrimSuffix(nameWithoutExt, ".gz")

		parts := strings.Split(nameWithoutExt, "_")
		if len(parts) >= 3 {
			// For format: full_dbname_20251001_224620.950678
			// We need the last two parts: 20251001 and 224620.950678
			timestampStr := parts[len(parts)-2] + "_" + parts[len(parts)-1]

			// Try parsing with microseconds first
			backupTime, err := time.Parse("20060102_150405.000000", timestampStr)
			if err != nil {
				// Fallback to format without microseconds
				backupTime, err = time.Parse("20060102_150405", timestampStr)
				if err != nil {
					LogWarn("Failed to parse timestamp from %s: %v", fileName, err)
					continue
				}
			}

			if backupTime.After(latestTime) {
				latestTime = backupTime
				latestBackup = fileName
			}
		}
	}

	if latestBackup == "" {
		LogInfo("No valid full backup timestamp found for %s - will do full backup", dbName)
		return "full"
	}

	LogDebug("Found latest full backup for %s: %s (timestamp: %s)", dbName, latestBackup, latestTime.Format("2006-01-02 15:04:05.000000"))

	// Check if the latest full backup is within the full backup interval
	intervalDays := config.Backup.FullBackupInterval
	cutoffTime := time.Now().AddDate(0, 0, -intervalDays)

	if latestTime.Before(cutoffTime) {
		LogInfo("Latest full backup for %s is %d days old (cutoff: %d days) - will do full backup",
			dbName, int(time.Since(latestTime).Hours()/24), intervalDays)
		return "full"
	}

	LogInfo("Latest full backup for %s is recent (%s) - file: %s - will do incremental backup",
		dbName, latestTime.Format("2006-01-02 15:04:05"), latestBackup)
	return "incremental"
}

// handleLoggingStatus returns the current logging system status
func handleLoggingStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if logger is initialized
	if appLogger == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Logging system not initialized",
			"status":  "error",
		})
		return
	}

	// Check if log directory is accessible
	logDir := appLogger.logDir
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Log directory does not exist: %s", logDir),
			"status":  "error",
		})
		return
	}

	// Check if log directory is writable
	testFile := filepath.Join(logDir, ".write_test")
	file, err := os.Create(testFile)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Log directory is not writable: %s", logDir),
			"status":  "error",
		})
		return
	}
	file.Close()
	os.Remove(testFile) // Clean up test file

	// Get current log file info
	currentDate := time.Now().Format("20060102")
	logFileName := fmt.Sprintf("mbt-%s.log", currentDate)
	logFilePath := filepath.Join(logDir, logFileName)

	// Check if current log file exists and get its size
	var logFileSize int64
	if stat, err := os.Stat(logFilePath); err == nil {
		logFileSize = stat.Size()
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"status":  "ok",
		"details": map[string]interface{}{
			"log_directory":    logDir,
			"current_log_file": logFileName,
			"log_file_path":    logFilePath,
			"log_file_size":    logFileSize,
			"retention_days":   appLogger.config.Logging.RetentionLogs,
		},
	})
}

// handleLogStream returns log entries with optional datetime filtering
func handleLogStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	date := r.URL.Query().Get("date")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	// Set default limit with reasonable maximum to prevent performance issues
	limit := 2000
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			// Cap the limit to prevent excessive memory usage
			if parsedLimit > 5000 {
				limit = 5000
			} else {
				limit = parsedLimit
			}
		}
	}

	// Parse offset parameter
	offset := 0
	if offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset > 0 {
			offset = parsedOffset
		}
	}

	// Get log entries for the specified date
	LogDebug("Getting log entries for date: %s, limit: %d, offset: %d", date, limit, offset)
	logEntries, err := getLogEntries(date, "", limit, offset)
	if err != nil {
		LogError("Failed to get log entries: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to get log entries: " + err.Error(),
		})
		return
	}

	LogDebug("Retrieved %d log entries", len(logEntries))

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"logs":    logEntries,
		"count":   len(logEntries),
	})
}

// handleDeleteLogFile handles deletion of log files
func handleDeleteLogFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var requestData struct {
		Date string `json:"date"`
	}

	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid request format: " + err.Error(),
		})
		return
	}

	// Validate date parameter
	if requestData.Date == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Date parameter is required",
		})
		return
	}

	// Parse date to validate format
	parsedDate, err := time.Parse("2006-01-02", requestData.Date)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid date format. Expected YYYY-MM-DD",
		})
		return
	}

	// Check if logger is initialized
	if appLogger == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Logging system not initialized",
		})
		return
	}

	// Construct log file path
	logFileName := fmt.Sprintf("mbt-%s.log", parsedDate.Format("20060102"))
	logFilePath := filepath.Join(appLogger.logDir, logFileName)

	// Check if log file exists
	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Log file not found for date %s", requestData.Date),
		})
		return
	}

	// Check if this is the current day's log file
	currentDate := time.Now().Format("2006-01-02")
	isCurrentDay := requestData.Date == currentDate

	if isCurrentDay {
		// For current day: clear the content of the file but keep the file
		file, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Failed to clear log file content for %s: %s", requestData.Date, err.Error()),
			})
			return
		}
		defer file.Close()

		// Truncate the file to 0 bytes (clear content)
		if err := file.Truncate(0); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Failed to clear log file content for %s: %s", requestData.Date, err.Error()),
			})
			return
		}

		LogInfo("Log file content cleared: %s", logFilePath)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Log file content for %s cleared successfully", requestData.Date),
			"file":    logFilePath,
			"action":  "cleared",
		})
	} else {
		// For non-current days: delete the entire file
		if err := os.Remove(logFilePath); err != nil {
			// Check if the error is due to file being in use
			if strings.Contains(err.Error(), "being used by another process") ||
				strings.Contains(err.Error(), "file is being used") ||
				strings.Contains(err.Error(), "access is denied") {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   fmt.Sprintf("Cannot delete log file for %s: The file is currently being used by another process. Please try again later or restart the application.", requestData.Date),
				})
				return
			}

			// Other deletion errors
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Failed to delete log file: " + err.Error(),
			})
			return
		}

		LogInfo("Log file deleted: %s", logFilePath)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Log file for %s deleted successfully", requestData.Date),
			"file":    logFilePath,
			"action":  "deleted",
		})
	}
}

// getLogEntries retrieves log entries with optional date filtering
func getLogEntries(date, endTime string, limit int, offset int) ([]map[string]interface{}, error) {
	if appLogger == nil {
		return nil, fmt.Errorf("logging system not initialized")
	}

	// Get log file for specific date or current date
	var logFileName string
	if date != "" {
		// Parse date from date parameter (format: YYYY-MM-DD)
		if parsedDate, err := time.Parse("2006-01-02", date); err == nil {
			logFileName = fmt.Sprintf("mbt-%s.log", parsedDate.Format("20060102"))
			LogDebug("Parsed date %s -> log file: %s", date, logFileName)
		} else {
			// Fallback to current date if parsing fails
			currentDate := time.Now().Format("20060102")
			logFileName = fmt.Sprintf("mbt-%s.log", currentDate)
			LogDebug("Failed to parse date %s, using current date: %s", date, logFileName)
		}
	} else {
		// Use current date if no date specified
		currentDate := time.Now().Format("20060102")
		logFileName = fmt.Sprintf("mbt-%s.log", currentDate)
		LogDebug("No date specified, using current date: %s", logFileName)
	}

	logFilePath := filepath.Join(appLogger.logDir, logFileName)

	LogDebug("Looking for log file: %s", logFilePath)

	// Check if log file exists
	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		LogDebug("Log file does not exist: %s", logFilePath)
		return []map[string]interface{}{}, nil
	}

	// Read log file
	content, err := os.ReadFile(logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read log file: %v", err)
	}

	// Parse log entries efficiently
	lines := strings.Split(string(content), "\n")
	var entries []map[string]interface{}
	entries = make([]map[string]interface{}, 0, limit) // Pre-allocate with capacity

	count := 0
	for _, line := range lines {
		// Skip lines until we reach the offset
		if count < offset {
			count++
			continue
		}

		// Stop if we've reached the limit
		if len(entries) >= limit {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse log entry
		entry := parseLogEntry(line)
		if entry == nil {
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// getNewLogEntries removed - no longer needed with direct WebSocket broadcasting

// parseLogEntry parses a single log line into structured data
func parseLogEntry(line string) map[string]interface{} {
	// Expected format: 15:04:05 [LEVEL] message
	// But also handle other formats gracefully

	// Try to find the log level pattern [LEVEL]
	levelStart := strings.Index(line, "[")
	levelEnd := strings.Index(line, "]")

	if levelStart == -1 || levelEnd == -1 || levelEnd <= levelStart {
		// No level found, treat as info level
		return map[string]interface{}{
			"level":     "INFO",
			"timestamp": time.Now().Format("15:04:05"),
			"message":   line,
			"color":     "info",
			"raw":       line,
		}
	}

	// Extract timestamp (everything before the level)
	timestamp := strings.TrimSpace(line[:levelStart])
	if timestamp == "" {
		timestamp = time.Now().Format("15:04:05")
	}

	// Extract level
	level := strings.TrimSpace(line[levelStart+1 : levelEnd])

	// Extract message (everything after the level)
	message := ""
	if levelEnd+1 < len(line) {
		message = strings.TrimSpace(line[levelEnd+1:])
	}

	// If no message, use the whole line
	if message == "" {
		message = line
	}

	// Determine log level color
	var levelColor string
	switch strings.ToUpper(level) {
	case "DEBUG":
		levelColor = "debug"
	case "INFO":
		levelColor = "info"
	case "WARN", "WARNING":
		levelColor = "warn"
	case "ERROR", "ERR":
		levelColor = "error"
	case "FATAL":
		levelColor = "error"
	default:
		levelColor = "info"
	}

	return map[string]interface{}{
		"level":     strings.ToUpper(level),
		"timestamp": timestamp,
		"message":   message,
		"color":     levelColor,
		"raw":       line,
	}
}

// matchesTimeFilter checks if a timestamp matches the time filter
func matchesTimeFilter(timestamp, startTime, endTime string) bool {
	// Parse timestamp (format: 15:04:05)
	entryTime, err := time.Parse("15:04:05", timestamp)
	if err != nil {
		return false
	}

	// Check start time filter
	if startTime != "" {
		start, err := time.Parse("2006-01-02T15:04:05", startTime)
		if err == nil && entryTime.Before(start) {
			return false
		}
	}

	// Check end time filter
	if endTime != "" {
		end, err := time.Parse("2006-01-02T15:04:05", endTime)
		if err == nil && entryTime.After(end) {
			return false
		}
	}

	return true
}

// handleLogDebug provides debug information about log streaming
func handleLogDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get current log file info
	currentDate := time.Now().Format("20060102")
	logFileName := fmt.Sprintf("mbt-%s.log", currentDate)
	logFilePath := filepath.Join(appLogger.logDir, logFileName)

	var fileSize int64
	var fileExists bool
	if stat, err := os.Stat(logFilePath); err == nil {
		fileSize = stat.Size()
		fileExists = true
	}

	debugInfo := map[string]interface{}{
		"success": true,
		"debug": map[string]interface{}{
			"current_log_file": map[string]interface{}{
				"filename":   logFileName,
				"filepath":   logFilePath,
				"exists":     fileExists,
				"size_bytes": fileSize,
				"size_mb":    float64(fileSize) / (1024 * 1024),
			},
			"websocket_connections": getWebSocketConnectionCounts(),
			"logger_initialized":    appLogger != nil,
		},
	}

	json.NewEncoder(w).Encode(debugInfo)
}

// handleClearBackupHistory handles clearing all backup history
func handleClearBackupHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	LogInfo("Clear backup history request received")

	// Clear all backup history from database
	err := ClearAllBackupHistory()
	if err != nil {
		LogError("Failed to clear backup history: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to clear backup history: " + err.Error(),
		})
		return
	}

	LogInfo("Successfully cleared all backup history")

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "All backup history has been cleared successfully",
	})
}

// handleDownloadBackup handles backup file downloads
func handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract job ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/backup/download/")
	if path == "" {
		http.Error(w, "Job ID required", http.StatusBadRequest)
		return
	}

	// Get job details from database
	job, err := GetBackupJobByID(path)
	if err != nil {
		LogError("Failed to get backup job %s: %v", path, err)
		http.Error(w, "Backup job not found", http.StatusNotFound)
		return
	}

	// Check if job is completed and has backup file path
	if job["status"] != "done" {
		http.Error(w, "Backup job not completed", http.StatusBadRequest)
		return
	}

	backupFilePath, ok := job["backup_file_path"].(string)
	if !ok || backupFilePath == "" {
		http.Error(w, "Backup file path not available", http.StatusNotFound)
		return
	}

	// Check if file exists
	if _, err := os.Stat(backupFilePath); os.IsNotExist(err) {
		LogError("Backup file not found: %s", backupFilePath)
		http.Error(w, "Backup file not found on disk", http.StatusNotFound)
		return
	}

	// Get file info for headers
	fileInfo, err := os.Stat(backupFilePath)
	if err != nil {
		LogError("Failed to get file info for %s: %v", backupFilePath, err)
		http.Error(w, "Failed to access backup file", http.StatusInternalServerError)
		return
	}

	// Set headers for file download
	// Extract original filename from backup file path
	originalFilename := filepath.Base(backupFilePath)

	// Use original filename for download, but ensure it has proper extension
	if !strings.HasSuffix(originalFilename, ".sql") && !strings.HasSuffix(originalFilename, ".gz") {
		originalFilename += ".sql"
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", originalFilename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	// Open and serve the file
	file, err := os.Open(backupFilePath)
	if err != nil {
		LogError("Failed to open backup file %s: %v", backupFilePath, err)
		http.Error(w, "Failed to open backup file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Copy file to response
	_, err = io.Copy(w, file)
	if err != nil {
		LogError("Failed to serve backup file %s: %v", backupFilePath, err)
		return
	}

	LogInfo("Successfully served backup file: %s (Job ID: %s)", backupFilePath, path)
}

// getLastBackupTime returns the timestamp of the most recent completed backup
func getLastBackupTime() string {
	jobs, err := GetBackupJobs()
	if err != nil || len(jobs) == 0 {
		return ""
	}

	// Find the most recent completed job
	for _, job := range jobs {
		if job["status"] == "done" && job["completed_at"] != nil {
			if completedAt, ok := job["completed_at"].(string); ok {
				return completedAt
			}
		}
	}

	return ""
}

// calculateNextBackupTime calculates the next scheduled backup time based on start time and interval
// This function now uses the shared calculation function from scheduler.go
func calculateNextBackupTime(startTime string, intervalHours int) string {
	return CalculateNextBackupTime(startTime, intervalHours)
}

// Global abort mechanism for backup processes
var globalBackupAbortFlag bool
var globalBackupAbortMutex sync.RWMutex

// SignalGlobalBackupAbort signals all backup processes to abort
func SignalGlobalBackupAbort() {
	globalBackupAbortMutex.Lock()
	globalBackupAbortFlag = true
	globalBackupAbortMutex.Unlock()
	LogInfo("Global backup abort signal sent")
}

// CheckGlobalBackupAbort checks if global abort has been signaled
func CheckGlobalBackupAbort() bool {
	globalBackupAbortMutex.RLock()
	defer globalBackupAbortMutex.RUnlock()
	return globalBackupAbortFlag
}

// ResetGlobalBackupAbort resets the global abort flag (call this when starting new backups)
func ResetGlobalBackupAbort() {
	globalBackupAbortMutex.Lock()
	globalBackupAbortFlag = false
	globalBackupAbortMutex.Unlock()
	LogInfo("Global backup abort flag reset")
}

// handleDetectBinary API endpoint to detect binary files automatically
func handleDetectBinary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Load current config
	config, err := loadConfig("config.json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to load config: " + err.Error(),
		})
		return
	}

	// Detect binary paths
	result := detectBinaryPaths(config)

	json.NewEncoder(w).Encode(result)
}

// detectBinaryPaths detects mysqldump, mysqlcheck, and mysqlbinlog paths across different systems
func detectBinaryPaths(config *Config) map[string]interface{} {
	result := map[string]interface{}{
		"success": false,
		"message": "Detection failed",
		"detected": map[string]interface{}{
			"mysqldump":   "",
			"mysqlcheck":  "",
			"mysqlbinlog": "",
		},
		"details": "",
	}

	var detectedPaths = make(map[string]string)
	var detectionLog []string

	// Define possible binary names for different systems and versions
	binaryNames := map[string][]string{
		"mysqldump":   {"mysqldump", "mariadb-dump", "mysql_dump"},
		"mysqlcheck":  {"mysqlcheck", "mariadb-check", "mysql_check"},
		"mysqlbinlog": {"mysqlbinlog", "mariadb-binlog", "mysql_binlog"},
	}

	// Define common installation paths for different systems
	commonPaths := []string{
		"/usr/bin",
		"/usr/local/bin",
		"/usr/sbin",
		"/usr/local/sbin",
		"/opt/mysql/bin",
		"/opt/mariadb/bin",
		"/opt/mysql/mysql*/bin",
		"/opt/mariadb/mariadb*/bin",
		"/usr/local/mysql/bin",
		"/usr/local/mariadb/bin",
		"/Applications/XAMPP/bin", // macOS XAMPP
		"/Applications/MAMP/bin",  // macOS MAMP
		"C:\\Program Files\\MySQL\\MySQL Server *\\bin",       // Windows MySQL
		"C:\\Program Files\\MariaDB *\\bin",                   // Windows MariaDB
		"C:\\xampp\\mysql\\bin",                               // Windows XAMPP
		"C:\\Program Files (x86)\\MySQL\\MySQL Server *\\bin", // Windows MySQL 32-bit
		"C:\\Program Files (x86)\\MariaDB *\\bin",             // Windows MariaDB 32-bit
	}

	// Detect each binary type
	for binaryType, names := range binaryNames {
		detectionLog = append(detectionLog, fmt.Sprintf("🔍 Detecting %s...", binaryType))

		found := false
		for _, name := range names {
			// Check common paths
			for _, path := range commonPaths {
				// Handle wildcard paths
				if strings.Contains(path, "*") {
					matches, err := filepath.Glob(path)
					if err != nil {
						continue
					}
					for _, match := range matches {
						fullPath := filepath.Join(match, name)
						if _, err := os.Stat(fullPath); err == nil {
							detectedPaths[binaryType] = fullPath
							detectionLog = append(detectionLog, fmt.Sprintf("  ✅ Found %s at: %s", binaryType, fullPath))
							found = true
							break
						}
					}
				} else {
					fullPath := filepath.Join(path, name)
					if _, err := os.Stat(fullPath); err == nil {
						detectedPaths[binaryType] = fullPath
						detectionLog = append(detectionLog, fmt.Sprintf("  ✅ Found %s at: %s", binaryType, fullPath))
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if found {
				break
			}
		}

		// Try using 'which' command on Unix systems
		if !found && (os.Getenv("OS") != "Windows_NT") {
			for _, name := range names {
				if path := findBinaryWithWhich(name); path != "" {
					detectedPaths[binaryType] = path
					detectionLog = append(detectionLog, fmt.Sprintf("  ✅ Found %s via which: %s", binaryType, path))
					found = true
					break
				}
			}
		}

		// Try using 'where' command on Windows
		if !found && os.Getenv("OS") == "Windows_NT" {
			for _, name := range names {
				if path := findBinaryWithWhere(name); path != "" {
					detectedPaths[binaryType] = path
					detectionLog = append(detectionLog, fmt.Sprintf("  ✅ Found %s via where: %s", binaryType, path))
					found = true
					break
				}
			}
		}

		if !found {
			detectionLog = append(detectionLog, fmt.Sprintf("  ❌ %s not found", binaryType))
		}
	}

	// Check if we found all required binaries
	requiredBinaries := []string{"mysqldump", "mysqlcheck", "mysqlbinlog"}
	foundCount := 0
	for _, binary := range requiredBinaries {
		if detectedPaths[binary] != "" {
			foundCount++
		}
	}

	if foundCount == len(requiredBinaries) {
		result["success"] = true
		result["message"] = fmt.Sprintf("Successfully detected all %d binary paths", foundCount)
		result["detected"] = detectedPaths
		result["details"] = strings.Join(detectionLog, "\n")
	} else if foundCount > 0 {
		result["success"] = false
		result["message"] = fmt.Sprintf("Partially detected %d of %d binary paths", foundCount, len(requiredBinaries))
		result["detected"] = detectedPaths
		result["details"] = strings.Join(detectionLog, "\n")
	} else {
		result["success"] = false
		result["message"] = "No binary paths detected"
		result["details"] = strings.Join(detectionLog, "\n")
	}

	return result
}

// findBinaryWithWhich uses the 'which' command to find a binary on Unix systems
func findBinaryWithWhich(binaryName string) string {
	cmd := exec.Command("which", binaryName)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	path := strings.TrimSpace(string(output))
	if path != "" && !strings.Contains(path, "not found") {
		return path
	}

	return ""
}

// findBinaryWithWhere uses the 'where' command to find a binary on Windows
func findBinaryWithWhere(binaryName string) string {
	cmd := exec.Command("where", binaryName)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) > 0 && lines[0] != "" {
		return strings.TrimSpace(lines[0])
	}

	return ""
}

// handleGetBackupTimeline returns backup timeline data for the last 30 days
func handleGetBackupTimeline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	daysStr := r.URL.Query().Get("days")
	days := 30 // default to 30 days
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 365 {
			days = d
		}
	}

	// Get timeline data from database
	timelineData, err := GetBackupTimelineData(days)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to fetch backup timeline data: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    timelineData,
		"days":    days,
	})
}
