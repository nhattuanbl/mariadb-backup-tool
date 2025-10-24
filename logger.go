package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogLevel represents different log levels
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

// LogEntry represents a single log entry
type LogEntry struct {
	Level     string
	Message   string
	Timestamp time.Time
}

// Logger represents our custom logger
type Logger struct {
	config     *Config
	debugLog   *log.Logger
	infoLog    *log.Logger
	warnLog    *log.Logger
	errorLog   *log.Logger
	currentLog *os.File
	logDir     string

	// Buffering
	buffer      []LogEntry
	bufferMutex sync.Mutex
	writeTicker *time.Ticker
	stopChan    chan bool
}

var appLogger *Logger

// Function pointer for broadcasting logs to WebSocket clients
// These are set by ws.go SetupLogBroadcasting() function
var broadcastLogEntry func(level, message string)                              // Used by ws.go
var broadcastLogEntryWithTime func(level, message string, timestamp time.Time) // Used by ws.go

func InitializeLogger(config *Config) error {
	if err := checkLogDirectory(config.Logging.LogDir); err != nil {
		return fmt.Errorf("log directory check failed: %v", err)
	}

	appLogger = &Logger{
		config:   config,
		logDir:   config.Logging.LogDir,
		buffer:   make([]LogEntry, 0, 100), // Pre-allocate buffer for 100 entries
		stopChan: make(chan bool),
	}

	if err := appLogger.openCurrentLogFile(); err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	if err := appLogger.cleanupOldLogs(); err != nil {
		appLogger.Error("Failed to cleanup old logs: %v", err)
	}

	appLogger.setupLoggers()
	appLogger.startBufferedWriter()

	appLogger.Info("Logging system initialized successfully")
	appLogger.Debug("Log directory: %s", config.Logging.LogDir)
	appLogger.Debug("Log retention: %d days", config.Logging.RetentionLogs)

	return nil
}

func checkLogDirectory(logDir string) error {
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory %s: %v", logDir, err)
		}
	}

	testFile := filepath.Join(logDir, ".write_test")
	file, err := os.Create(testFile)
	if err != nil {
		return fmt.Errorf("log directory %s is not writable: %v", logDir, err)
	}
	file.Close()
	os.Remove(testFile)

	return nil
}

func (l *Logger) openCurrentLogFile() error {
	if l.currentLog != nil {
		l.currentLog.Close()
	}

	dateStr := time.Now().Format("20060102")
	logFileName := fmt.Sprintf("mbt-%s.log", dateStr)
	logFilePath := filepath.Join(l.logDir, logFileName)

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %v", logFilePath, err)
	}

	l.currentLog = file
	return nil
}

func (l *Logger) setupLoggers() {
	// Only write to stdout for immediate console output
	// File writing will be handled by the buffered writer
	timeFlags := log.Ltime
	l.debugLog = log.New(os.Stdout, "", timeFlags)
	l.infoLog = log.New(os.Stdout, "", timeFlags)
	l.warnLog = log.New(os.Stdout, "", timeFlags)
	l.errorLog = log.New(os.Stdout, "", timeFlags)
}

// startBufferedWriter starts the background goroutine for buffered file writing
func (l *Logger) startBufferedWriter() {
	l.writeTicker = time.NewTicker(2 * time.Second) // Write every 2 seconds

	go func() {
		for {
			select {
			case <-l.writeTicker.C:
				l.flushBuffer()
			case <-l.stopChan:
				l.flushBuffer() // Flush remaining logs before stopping
				return
			}
		}
	}()
}

// flushBuffer writes all buffered log entries to file
func (l *Logger) flushBuffer() {
	l.bufferMutex.Lock()
	defer l.bufferMutex.Unlock()

	if len(l.buffer) == 0 {
		return
	}

	// Write all buffered entries to file
	for _, entry := range l.buffer {
		logLine := fmt.Sprintf("%s [%s] %s\n",
			entry.Timestamp.Format("15:04:05"),
			entry.Level,
			entry.Message)
		l.currentLog.WriteString(logLine)
	}

	// Clear buffer
	l.buffer = l.buffer[:0]

	// Sync to ensure data is written to disk
	l.currentLog.Sync()
}

// addToBuffer adds a log entry to the buffer
func (l *Logger) addToBuffer(level, message string) {
	l.bufferMutex.Lock()
	defer l.bufferMutex.Unlock()

	now := time.Now()
	entry := LogEntry{
		Level:     level,
		Message:   message,
		Timestamp: now,
	}

	l.buffer = append(l.buffer, entry)

	// Broadcast to WebSocket with the same timestamp
	broadcastLogToWebSocketWithTime(level, message, now)

	// If buffer gets too large, flush immediately
	if len(l.buffer) >= 50 {
		go l.flushBuffer()
	}
}

// checkAndRotateLogFile checks if we need to rotate to a new log file
func (l *Logger) checkAndRotateLogFile() {
	// Check if current date has changed
	currentDate := time.Now().Format("20060102")
	expectedFileName := fmt.Sprintf("mbt-%s.log", currentDate)
	currentFileName := filepath.Base(l.currentLog.Name())

	if currentFileName != expectedFileName {
		// Check if the log file for today already exists
		logFilePath := filepath.Join(l.logDir, expectedFileName)

		// Only rotate if the file doesn't exist (new day)
		if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
			// Date has changed and file doesn't exist, rotate log file
			// Use direct file operations to avoid recursion
			l.rotateLogFileDirectly(currentDate)
		} else {
			// File exists, just switch to it without logging
			if err := l.openCurrentLogFile(); err != nil {
				// Only log error if we can't open the existing file
				l.writeLogDirectly("ERROR", fmt.Sprintf("Failed to open existing log file: %v", err))
				return
			}
		}
	}
}

// cleanupOldLogs removes log files older than the retention period
func (l *Logger) cleanupOldLogs() error {
	retentionDays := l.config.Logging.RetentionLogs
	if retentionDays <= 0 {
		return nil // No cleanup needed
	}

	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)

	// Read log directory
	files, err := os.ReadDir(l.logDir)
	if err != nil {
		return fmt.Errorf("failed to read log directory: %v", err)
	}

	removedCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Check if file matches our log file pattern: mbt-YYYYMMDD.log
		fileName := file.Name()
		if !strings.HasPrefix(fileName, "mbt-") || !strings.HasSuffix(fileName, ".log") {
			continue
		}

		// Extract date from filename
		dateStr := strings.TrimPrefix(strings.TrimSuffix(fileName, ".log"), "mbt-")
		if len(dateStr) != 8 {
			continue // Invalid date format
		}

		// Parse date
		fileDate, err := time.Parse("20060102", dateStr)
		if err != nil {
			continue // Invalid date
		}

		// Check if file is older than retention period
		if fileDate.Before(cutoffDate) {
			filePath := filepath.Join(l.logDir, fileName)
			if err := os.Remove(filePath); err != nil {
				l.Error("Failed to remove old log file %s: %v", fileName, err)
			} else {
				removedCount++
				l.Debug("Removed old log file: %s", fileName)
			}
		}
	}

	if removedCount > 0 {
		l.Info("Cleaned up %d old log files (older than %d days)", removedCount, retentionDays)
	}

	return nil
}

// Logging methods
func (l *Logger) Debug(format string, v ...interface{}) {
	l.checkAndRotateLogFile()
	message := fmt.Sprintf(format, v...)
	l.debugLog.Printf("[DEBUG] " + message)
	l.addToBuffer("DEBUG", message)
}

func (l *Logger) Info(format string, v ...interface{}) {
	l.checkAndRotateLogFile()
	message := fmt.Sprintf(format, v...)
	l.infoLog.Printf("[INFO] " + message)
	l.addToBuffer("INFO", message)
}

func (l *Logger) Warn(format string, v ...interface{}) {
	l.checkAndRotateLogFile()
	message := fmt.Sprintf(format, v...)
	l.warnLog.Printf("[WARN] " + message)
	l.addToBuffer("WARN", message)
}

func (l *Logger) Error(format string, v ...interface{}) {
	l.checkAndRotateLogFile()
	message := fmt.Sprintf(format, v...)
	l.errorLog.Printf("[ERROR] " + message)
	l.addToBuffer("ERROR", message)
}

// GetLogger returns the global logger instance
func GetLogger() *Logger {
	return appLogger
}

// LogWithLevel logs a message with the specified level
func LogWithLevel(level LogLevel, format string, v ...interface{}) {
	if appLogger == nil {
		// Fallback to standard log if logger not initialized
		log.Printf(format, v...)
		return
	}

	switch level {
	case DEBUG:
		appLogger.Debug(format, v...)
	case INFO:
		appLogger.Info(format, v...)
	case WARN:
		appLogger.Warn(format, v...)
	case ERROR:
		appLogger.Error(format, v...)
	}
}

// LogDebug is a convenience function for debug logging
func LogDebug(format string, v ...interface{}) {
	LogWithLevel(DEBUG, format, v...)
}

// LogInfo is a convenience function for info logging
func LogInfo(format string, v ...interface{}) {
	LogWithLevel(INFO, format, v...)
}

// LogWarn is a convenience function for warning logging
func LogWarn(format string, v ...interface{}) {
	LogWithLevel(WARN, format, v...)
}

// LogError is a convenience function for error logging
func LogError(format string, v ...interface{}) {
	LogWithLevel(ERROR, format, v...)
}

// ShutdownLogger properly shuts down the logger and flushes remaining logs
func ShutdownLogger() {
	if appLogger != nil {
		appLogger.stopChan <- true
		if appLogger.writeTicker != nil {
			appLogger.writeTicker.Stop()
		}
	}
}

// broadcastLogToWebSocket sends log entry directly to WebSocket clients
func broadcastLogToWebSocket(level, message string) {
	// This will be called from ws.go to avoid circular imports
	if broadcastLogEntry != nil {
		broadcastLogEntry(level, message)
	}
}

// broadcastLogToWebSocketWithTime sends log entry with specific timestamp to WebSocket clients
func broadcastLogToWebSocketWithTime(level, message string, timestamp time.Time) {
	// This will be called from ws.go to avoid circular imports
	if broadcastLogEntryWithTime != nil {
		broadcastLogEntryWithTime(level, message, timestamp)
	}
}

// rotateLogFileDirectly handles log file rotation without recursion
func (l *Logger) rotateLogFileDirectly(dateStr string) {
	// Close current log file
	if l.currentLog != nil {
		l.currentLog.Close()
	}

	// Open new log file
	logFileName := fmt.Sprintf("mbt-%s.log", dateStr)
	logFilePath := filepath.Join(l.logDir, logFileName)

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Use direct output instead of logging to avoid recursion
		fmt.Printf("[ERROR] Failed to create new log file %s: %v\n", logFilePath, err)
		return
	}

	l.currentLog = file

	// Write rotation message directly to file
	now := time.Now()
	logLine := fmt.Sprintf("%s [INFO] Rotating log file for new date: %s\n",
		now.Format("15:04:05"),
		dateStr)
	l.currentLog.WriteString(logLine)
	l.currentLog.Sync()

	// Also output to console
	fmt.Printf("[INFO] Rotating log file for new date: %s\n", dateStr)
}

// writeLogDirectly writes logs without triggering rotation checks
func (l *Logger) writeLogDirectly(level, message string) {
	now := time.Now()

	// Write to console
	fmt.Printf("[%s] %s\n", level, message)

	// Write to file if currentLog is available
	if l.currentLog != nil {
		logLine := fmt.Sprintf("%s [%s] %s\n",
			now.Format("15:04:05"),
			level,
			message)
		l.currentLog.WriteString(logLine)
		l.currentLog.Sync()
	}
}
