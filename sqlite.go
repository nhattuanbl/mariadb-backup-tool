package main

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var db *sql.DB
var dbMutex sync.RWMutex
var operationQueue chan func()
var queueWorkerStarted bool

// Batch update system for progress updates
type ProgressUpdate struct {
	JobID        string
	DatabaseName string
	Progress     int
	Timestamp    time.Time
}

var progressUpdateQueue chan ProgressUpdate
var progressBatchSize = 10
var progressBatchTimeout = 2 * time.Second

// Batch update system for status updates
type StatusUpdate struct {
	JobID        string
	DatabaseName string
	Status       string
	Timestamp    time.Time
}

var statusUpdateQueue chan StatusUpdate
var statusBatchTimeout = 1 * time.Second //persistant update

// Database operation metrics
type DatabaseMetrics struct {
	TotalOperations   int64
	FailedOperations  int64
	RetryOperations   int64
	LockContentionOps int64
	BatchOperations   int64
	LastResetTime     time.Time
	mutex             sync.RWMutex
}

var dbMetrics DatabaseMetrics

func InitDB(dbFile string) error {
	var err error
	db, err = sql.Open("sqlite", dbFile)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}

	// Configure connection pool for better concurrency
	db.SetMaxOpenConns(50)   // Increased from 25 to handle more concurrent operations
	db.SetMaxIdleConns(20)   // Increased from 10 to keep more connections ready
	db.SetConnMaxLifetime(0) // Connections don't expire

	// Configure SQLite for better concurrency and reduced locking
	concurrencySettings := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",             // WAL mode allows concurrent reads
		"PRAGMA synchronous = NORMAL",           // Faster than FULL, still safe
		"PRAGMA cache_size = 20000",             // Increased cache size for better performance
		"PRAGMA temp_store = MEMORY",            // Store temp tables in memory
		"PRAGMA busy_timeout = 60000",           // Increased to 60 seconds for locks
		"PRAGMA wal_autocheckpoint = 2000",      // Increased checkpoint frequency
		"PRAGMA journal_size_limit = 134217728", // Increased to 128MB journal size limit
		"PRAGMA mmap_size = 268435456",          // 256MB memory-mapped I/O
		"PRAGMA page_size = 4096",               // Standard page size
		"PRAGMA locking_mode = NORMAL",          // Allow multiple readers
		"PRAGMA read_uncommitted = 1",           // Allow dirty reads to reduce blocking
	}

	for _, setting := range concurrencySettings {
		_, err = db.Exec(setting)
		if err != nil {
			return fmt.Errorf("failed to set %s: %v", setting, err)
		}
	}

	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %v", err)
	}

	// Initialize operation queue for serializing critical database operations
	operationQueue = make(chan func(), 1000) // Buffer for 1000 operations
	startQueueWorker()

	// Initialize progress update batching system
	progressUpdateQueue = make(chan ProgressUpdate, 500) // Buffer for 500 progress updates
	startProgressBatchWorker()

	// Initialize status update batching system
	statusUpdateQueue = make(chan StatusUpdate, 500) // Buffer for 500 status updates
	startStatusBatchWorker()

	// Initialize metrics
	dbMetrics.LastResetTime = time.Now()

	LogInfo("SQLite database initialized at %s with enhanced concurrency settings", dbFile)
	return nil
}

// startQueueWorker starts a background worker to process database operations serially
func startQueueWorker() {
	if queueWorkerStarted {
		return
	}
	queueWorkerStarted = true

	go func() {
		for operation := range operationQueue {
			func() {
				defer func() {
					if r := recover(); r != nil {
						LogError("Database operation panic recovered: %v", r)
					}
				}()
				operation()
			}()
		}
	}()
}

// executeWithRetry executes a database operation with exponential backoff and jitter
func executeWithRetry(operation func() error, operationName string, maxRetries int) error {
	if maxRetries <= 0 {
		maxRetries = 5
	}

	// Record operation start
	dbMetrics.mutex.Lock()
	dbMetrics.TotalOperations++
	dbMetrics.mutex.Unlock()

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil // Success
		}

		// Check if it's a locking error
		if strings.Contains(err.Error(), "database is locked") ||
			strings.Contains(err.Error(), "SQLITE_BUSY") {

			// Record lock contention
			dbMetrics.mutex.Lock()
			dbMetrics.LockContentionOps++
			if attempt > 1 {
				dbMetrics.RetryOperations++
			}
			dbMetrics.mutex.Unlock()

			if attempt == maxRetries {
				dbMetrics.mutex.Lock()
				dbMetrics.FailedOperations++
				dbMetrics.mutex.Unlock()
				return fmt.Errorf("%s failed after %d attempts: %v", operationName, maxRetries, err)
			}

			// Exponential backoff with jitter
			baseDelay := time.Duration(attempt) * 100 * time.Millisecond
			jitter := time.Duration(rand.Intn(50)) * time.Millisecond
			delay := baseDelay + jitter

			LogWarn("%s attempt %d failed due to lock contention, retrying in %v: %v",
				operationName, attempt, delay, err)
			time.Sleep(delay)
			continue
		}

		// For non-locking errors, record failure and return immediately
		dbMetrics.mutex.Lock()
		dbMetrics.FailedOperations++
		dbMetrics.mutex.Unlock()
		return fmt.Errorf("%s failed: %v", operationName, err)
	}

	return fmt.Errorf("unexpected error in retry loop")
}

// queueOperation queues a database operation for serial execution
func queueOperation(operation func()) {
	select {
	case operationQueue <- operation:
		// Operation queued successfully
	default:
		// Queue is full, execute immediately with mutex protection
		dbMutex.Lock()
		defer dbMutex.Unlock()
		operation()
	}
}

// startProgressBatchWorker starts a background worker to batch progress updates
func startProgressBatchWorker() {
	go func() {
		var batch []ProgressUpdate
		ticker := time.NewTicker(progressBatchTimeout)
		defer ticker.Stop()

		for {
			select {
			case update := <-progressUpdateQueue:
				batch = append(batch, update)

				// Process batch if it reaches the batch size
				if len(batch) >= progressBatchSize {
					processProgressBatch(batch)
					batch = batch[:0] // Reset batch
				}

			case <-ticker.C:
				// Process any remaining updates in the batch
				if len(batch) > 0 {
					processProgressBatch(batch)
					batch = batch[:0] // Reset batch
				}
			}
		}
	}()
}

// processProgressBatch processes a batch of progress updates
func processProgressBatch(batch []ProgressUpdate) {
	if len(batch) == 0 {
		return
	}

	// Group updates by job_id and database_name to avoid duplicate updates
	updateMap := make(map[string]ProgressUpdate)
	for _, update := range batch {
		key := update.JobID + "|" + update.DatabaseName
		// Keep the most recent update for each job/database combination
		if existing, exists := updateMap[key]; !exists || update.Timestamp.After(existing.Timestamp) {
			updateMap[key] = update
		}
	}

	// Execute batch update
	query := `UPDATE backup_jobs SET progress = ? WHERE job_id = ? AND database_name = ?`

	err := executeWithRetry(func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		for _, update := range updateMap {
			_, err := tx.Exec(query, update.Progress, update.JobID, update.DatabaseName)
			if err != nil {
				return err
			}
		}

		return tx.Commit()
	}, fmt.Sprintf("BatchProgressUpdate(%d updates)", len(updateMap)), 3)

	if err != nil {
		LogError("Failed to process progress batch: %v", err)
	} else {
		// Broadcast job update to UI asynchronously
		go broadcastJobsUpdate()

		// Record batch operation metrics
		dbMetrics.mutex.Lock()
		dbMetrics.BatchOperations++
		dbMetrics.mutex.Unlock()
	}
}

// startStatusBatchWorker starts a background worker to batch status updates
func startStatusBatchWorker() {
	go func() {
		var batch []StatusUpdate
		ticker := time.NewTicker(statusBatchTimeout)
		defer ticker.Stop()

		for {
			select {
			case update := <-statusUpdateQueue:
				batch = append(batch, update)

				// Process batch immediately if it's a critical status (done/failed)
				// These statuses are important and should be persisted quickly
				if update.Status == "done" || update.Status == "failed" {
					// Process all pending updates including this critical one
					processStatusBatch(batch)
					batch = batch[:0] // Reset batch
				}

			case <-ticker.C:
				// Process any remaining updates in the batch every 2 seconds
				if len(batch) > 0 {
					processStatusBatch(batch)
					batch = batch[:0] // Reset batch
				}
			}
		}
	}()
}

// processStatusBatch processes a batch of status updates
func processStatusBatch(batch []StatusUpdate) {
	if len(batch) == 0 {
		return
	}

	// Group updates by job_id and database_name, keeping the most recent status for each
	updateMap := make(map[string]StatusUpdate)
	for _, update := range batch {
		key := update.JobID + "|" + update.DatabaseName
		// Keep the most recent update for each job/database combination
		if existing, exists := updateMap[key]; !exists || update.Timestamp.After(existing.Timestamp) {
			updateMap[key] = update
		}
	}

	// Execute batch update with transaction
	err := executeWithRetry(func() error {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		for _, update := range updateMap {
			var query string
			if update.Status == "done" || update.Status == "failed" {
				query = `UPDATE backup_jobs SET status = ?, completed_at = CURRENT_TIMESTAMP WHERE job_id = ? AND database_name = ?`
				_, err := tx.Exec(query, update.Status, update.JobID, update.DatabaseName)
				if err != nil {
					return err
				}
			} else {
				query = `UPDATE backup_jobs SET status = ? WHERE job_id = ? AND database_name = ?`
				_, err := tx.Exec(query, update.Status, update.JobID, update.DatabaseName)
				if err != nil {
					return err
				}
			}
		}

		return tx.Commit()
	}, fmt.Sprintf("BatchStatusUpdate(%d updates)", len(updateMap)), 3)

	if err != nil {
		LogError("Failed to process status batch: %v", err)
	} else {
		// Broadcast job update to UI asynchronously
		go broadcastJobsUpdate()

		// Record batch operation metrics
		dbMetrics.mutex.Lock()
		dbMetrics.BatchOperations++
		dbMetrics.mutex.Unlock()
	}
}

func createTables() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS backup_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			database_name TEXT NOT NULL,
			backup_type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'running',
			progress INTEGER DEFAULT 0,
			started_at DATETIME,
			completed_at DATETIME,
			estimated_size_kb INTEGER,
			actual_size_kb INTEGER,
			backup_file_path TEXT,
			error_message TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS backup_summary (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT UNIQUE NOT NULL,
			total_db_count INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			state TEXT NOT NULL DEFAULT 'running',
			completed_at DATETIME,
			total_size_kb INTEGER DEFAULT 0,
			total_disk_size INTEGER DEFAULT 0,
			backup_mode TEXT NOT NULL,
			total_full INTEGER DEFAULT 0,
			total_incremental INTEGER DEFAULT 0,
			total_failed INTEGER DEFAULT 0,
			mysql_restart_time INTEGER DEFAULT 0
		)`,
	}

	//backup_mode (auto, full, incremental)
	//backup_type (auto-full, auto-inc, force-full, force-inc)
	//status (running, done, failed, cancelled, optimizing)

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query: %v", err)
		}
	}

	return nil
}

func GetDB() *sql.DB {
	return db
}

// Backup Jobs Functions
func CreateBackupJob(jobID, databaseName, backupType string, estimatedSizeKB int) error {
	query := `INSERT INTO backup_jobs (job_id, database_name, backup_type, status, progress, started_at, estimated_size_kb)
		VALUES (?, ?, ?, 'running', 0, CURRENT_TIMESTAMP, ?)`

	return executeWithRetry(func() error {
		_, err := db.Exec(query, jobID, databaseName, backupType, estimatedSizeKB)
		return err
	}, fmt.Sprintf("CreateBackupJob(%s/%s)", jobID, databaseName), 5)
}

func UpdateBackupJobProgress(jobID, databaseName string, progress int) error {
	// Queue the progress update for batching instead of immediate execution
	select {
	case progressUpdateQueue <- ProgressUpdate{
		JobID:        jobID,
		DatabaseName: databaseName,
		Progress:     progress,
		Timestamp:    time.Now(),
	}:
		return nil // Successfully queued
	default:
		// Queue is full, fall back to immediate update with retry
		return executeWithRetry(func() error {
			query := `UPDATE backup_jobs SET progress = ? WHERE job_id = ? AND database_name = ?`
			_, err := db.Exec(query, progress, jobID, databaseName)
			if err == nil {
				// Broadcast job update to UI asynchronously to avoid blocking
				go broadcastJobsUpdate()
			}
			return err
		}, fmt.Sprintf("UpdateBackupJobProgress(%s/%s)", jobID, databaseName), 3)
	}
}

func UpdateBackupJobStatusByDB(jobID, databaseName, status string) error {
	// Queue the status update for batching instead of immediate execution
	select {
	case statusUpdateQueue <- StatusUpdate{
		JobID:        jobID,
		DatabaseName: databaseName,
		Status:       status,
		Timestamp:    time.Now(),
	}:
		return nil // Successfully queued
	default:
		// Queue is full, fall back to immediate update with retry
		var query string
		var args []interface{}

		if status == "done" || status == "failed" {
			query = `UPDATE backup_jobs SET status = ?, completed_at = CURRENT_TIMESTAMP WHERE job_id = ? AND database_name = ?`
			args = []interface{}{status, jobID, databaseName}
		} else {
			query = `UPDATE backup_jobs SET status = ? WHERE job_id = ? AND database_name = ?`
			args = []interface{}{status, jobID, databaseName}
		}

		return executeWithRetry(func() error {
			_, err := db.Exec(query, args...)
			if err == nil {
				// Broadcast job update to UI asynchronously to avoid blocking
				go broadcastJobsUpdate()
			}
			return err
		}, fmt.Sprintf("UpdateBackupJobStatusByDB(%s/%s)", jobID, databaseName), 3)
	}
}

func UpdateBackupJobError(jobID, databaseName, errorMessage string) error {
	query := `UPDATE backup_jobs SET error_message = ? WHERE job_id = ? AND database_name = ?`

	return executeWithRetry(func() error {
		_, err := db.Exec(query, errorMessage, jobID, databaseName)
		if err == nil {
			// Broadcast job update to UI asynchronously to avoid blocking
			go broadcastJobsUpdate()
		}
		return err
	}, fmt.Sprintf("UpdateBackupJobError(%s/%s)", jobID, databaseName), 3)
}

func getExistingJobError(jobID, databaseName string) string {
	query := `SELECT error_message FROM backup_jobs WHERE job_id = ? AND database_name = ?`
	var errorMessage sql.NullString
	err := db.QueryRow(query, jobID, databaseName).Scan(&errorMessage)
	if err != nil {
		return ""
	}
	if errorMessage.Valid {
		return errorMessage.String
	}
	return ""
}

func CompleteBackupJob(jobID, databaseName string, success bool, actualSizeKB int, backupFilePath, errorMessage string) error {
	status := "done"
	if !success {
		status = "failed"
	}

	query := `UPDATE backup_jobs 
		SET status = ?, completed_at = CURRENT_TIMESTAMP, 
			actual_size_kb = ?, backup_file_path = ?, error_message = ?
		WHERE job_id = ? AND database_name = ?`

	return executeWithRetry(func() error {
		result, err := db.Exec(query, status, actualSizeKB, backupFilePath, errorMessage, jobID, databaseName)
		if err != nil {
			return err
		}

		// Check if any rows were affected
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %v", err)
		}

		if rowsAffected == 0 {
			return fmt.Errorf("no backup job found with job_id=%s and database_name=%s", jobID, databaseName)
		}

		// Success - broadcast job update to UI asynchronously to avoid blocking
		go broadcastJobsUpdate()
		return nil
	}, fmt.Sprintf("CompleteBackupJob(%s/%s)", jobID, databaseName), 3)
}

// GetRunningJobs returns all currently running backup jobs
func GetRunningJobs() ([]map[string]interface{}, error) {
	query := `SELECT id, job_id, database_name, backup_type, status, progress, 
		started_at, completed_at, estimated_size_kb, actual_size_kb, backup_file_path, error_message
		FROM backup_jobs 
		WHERE status = 'running'
		ORDER BY started_at DESC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []map[string]interface{}
	for rows.Next() {
		var id int
		var jobID, databaseName, backupType, status string
		var progress, estimatedSizeKB, actualSizeKB sql.NullInt64
		var startedAt, completedAt sql.NullString
		var backupFilePath, errorMessage sql.NullString

		err := rows.Scan(&id, &jobID, &databaseName, &backupType, &status, &progress,
			&startedAt, &completedAt, &estimatedSizeKB, &actualSizeKB, &backupFilePath, &errorMessage)
		if err != nil {
			return nil, err
		}

		job := map[string]interface{}{
			"id":                id,
			"job_id":            jobID,
			"database_name":     databaseName,
			"backup_type":       backupType,
			"status":            status,
			"progress":          progress.Int64,
			"started_at":        startedAt.String,
			"completed_at":      completedAt.String,
			"estimated_size_kb": estimatedSizeKB.Int64,
			"actual_size_kb":    actualSizeKB.Int64,
			"backup_file_path":  backupFilePath.String,
			"error_message":     errorMessage.String,
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// GetActiveJobs returns all currently active backup jobs (running, optimizing, etc.)
func GetActiveJobs() ([]map[string]interface{}, error) {
	query := `SELECT id, job_id, database_name, backup_type, status, progress, 
		started_at, completed_at, estimated_size_kb, actual_size_kb, backup_file_path, error_message
		FROM backup_jobs 
		WHERE status IN ('running', 'optimizing')
		ORDER BY started_at DESC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []map[string]interface{}
	for rows.Next() {
		var id int
		var jobID, databaseName, backupType, status string
		var progress, estimatedSizeKB, actualSizeKB sql.NullInt64
		var startedAt, completedAt sql.NullString
		var backupFilePath, errorMessage sql.NullString

		err := rows.Scan(&id, &jobID, &databaseName, &backupType, &status, &progress,
			&startedAt, &completedAt, &estimatedSizeKB, &actualSizeKB, &backupFilePath, &errorMessage)
		if err != nil {
			return nil, err
		}

		job := map[string]interface{}{
			"id":                id,
			"job_id":            jobID,
			"database_name":     databaseName,
			"backup_type":       backupType,
			"status":            status,
			"progress":          progress.Int64,
			"started_at":        startedAt.String,
			"completed_at":      completedAt.String,
			"estimated_size_kb": estimatedSizeKB.Int64,
			"actual_size_kb":    actualSizeKB.Int64,
			"backup_file_path":  backupFilePath.String,
			"error_message":     errorMessage.String,
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// UpdateBackupJobStatus updates the status of a backup job
func UpdateBackupJobStatus(jobID, status, errorMessage string, success bool) error {
	var query string
	var args []interface{}

	if status == "cancelled" || status == "failed" {
		query = `UPDATE backup_jobs 
			SET status = ?, completed_at = CURRENT_TIMESTAMP, error_message = ?
			WHERE job_id = ?`
		args = []interface{}{status, errorMessage, jobID}
	} else {
		query = `UPDATE backup_jobs 
			SET status = ?, error_message = ?
			WHERE job_id = ?`
		args = []interface{}{status, errorMessage, jobID}
	}

	_, err := db.Exec(query, args...)
	return err
}

func GetBackupJobs() ([]map[string]interface{}, error) {
	query := `SELECT id, job_id, database_name, backup_type, status, progress, 
		started_at, completed_at, estimated_size_kb, actual_size_kb, backup_file_path, error_message
		FROM backup_jobs 
		ORDER BY 
			CASE 
				WHEN status = 'running' THEN 1
				WHEN status = 'done' THEN 2
				WHEN status = 'failed' THEN 3
				ELSE 4
			END,
			started_at DESC 
		LIMIT 50`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []map[string]interface{}
	for rows.Next() {
		var id int
		var jobID, databaseName, backupType, status string
		var progress, estimatedSizeKB, actualSizeKB sql.NullInt64
		var startedAt, completedAt, backupFilePath, errorMessage sql.NullString

		err := rows.Scan(&id, &jobID, &databaseName, &backupType, &status, &progress,
			&startedAt, &completedAt, &estimatedSizeKB, &actualSizeKB, &backupFilePath, &errorMessage)
		if err != nil {
			return nil, err
		}

		// Convert sql.NullString to regular string
		startedAtStr := ""
		if startedAt.Valid {
			startedAtStr = startedAt.String
		}
		completedAtStr := ""
		if completedAt.Valid {
			completedAtStr = completedAt.String
		}
		backupFilePathStr := ""
		if backupFilePath.Valid {
			backupFilePathStr = backupFilePath.String
		}
		errorMessageStr := ""
		if errorMessage.Valid {
			errorMessageStr = errorMessage.String
		}

		job := map[string]interface{}{
			"id":                id,
			"job_id":            jobID,
			"database_name":     databaseName,
			"backup_type":       backupType,
			"status":            status,
			"progress":          progress.Int64,
			"started_at":        startedAtStr,
			"completed_at":      completedAtStr,
			"estimated_size_kb": estimatedSizeKB.Int64,
			"actual_size_kb":    actualSizeKB.Int64,
			"backup_file_path":  backupFilePathStr,
			"error_message":     errorMessageStr,
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// Backup Summary Functions
func CreateBackupSummary(jobID, backupMode string, totalDBCount int) error {
	query := `INSERT INTO backup_summary (job_id, backup_mode, total_db_count, state)
		VALUES (?, ?, ?, 'running')`

	return executeWithRetry(func() error {
		_, err := db.Exec(query, jobID, backupMode, totalDBCount)
		return err
	}, fmt.Sprintf("CreateBackupSummary(%s)", jobID), 5)
}

func UpdateBackupSummary(jobID string, totalSizeKB, totalDiskSizeKB, totalFull, totalIncremental, totalFailed int) error {
	query := `UPDATE backup_summary 
		SET total_size_kb = ?, total_disk_size = ?, total_full = ?, total_incremental = ?, total_failed = ?
		WHERE job_id = ?`

	return executeWithRetry(func() error {
		_, err := db.Exec(query, totalSizeKB, totalDiskSizeKB, totalFull, totalIncremental, totalFailed, jobID)
		return err
	}, fmt.Sprintf("UpdateBackupSummary(%s)", jobID), 3)
}

func UpdateMySQLRestartTime(jobID string, restartTimeSeconds int) error {
	query := `UPDATE backup_summary 
		SET mysql_restart_time = ?
		WHERE job_id = ?`

	return executeWithRetry(func() error {
		_, err := db.Exec(query, restartTimeSeconds, jobID)
		return err
	}, fmt.Sprintf("UpdateMySQLRestartTime(%s)", jobID), 3)
}

func CompleteBackupSummary(jobID string, cfg *Config) error {
	query := `UPDATE backup_summary 
		SET state = 'completed', completed_at = CURRENT_TIMESTAMP
		WHERE job_id = ?`

	_, err := db.Exec(query, jobID)
	if err != nil {
		return err
	}

	// Send Slack notification if configured
	go func() {
		if cfg != nil && cfg.Notification.SlackWebhookURL != "" {
			if err := SendSlackNotificationFromSummary(cfg.Notification.SlackWebhookURL, jobID); err != nil {
				LogError("Failed to send Slack notification for job %s: %v", jobID, err)
			}
		}
	}()

	return nil
}

func CancelBackupSummary(jobID string) error {
	query := `UPDATE backup_summary 
		SET state = 'cancelled', completed_at = CURRENT_TIMESTAMP
		WHERE job_id = ?`

	_, err := db.Exec(query, jobID)
	return err
}

func GetBackupSummaries() ([]map[string]interface{}, error) {
	query := `SELECT job_id, total_db_count, created_at, state, completed_at, 
		total_size_kb, total_disk_size, backup_mode, total_full, total_incremental, total_failed, mysql_restart_time
		FROM backup_summary 
		ORDER BY created_at DESC 
		LIMIT 20`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []map[string]interface{}
	for rows.Next() {
		var jobID, createdAt, state, backupMode string
		var totalDBCount, totalSizeKB, totalDiskSizeKB, totalFull, totalIncremental, totalFailed, mysqlRestartTime int
		var completedAt sql.NullString // Use sql.NullString for nullable column

		err := rows.Scan(&jobID, &totalDBCount, &createdAt, &state, &completedAt,
			&totalSizeKB, &totalDiskSizeKB, &backupMode, &totalFull, &totalIncremental, &totalFailed, &mysqlRestartTime)
		if err != nil {
			return nil, err
		}

		// Convert sql.NullString to regular string
		completedAtStr := ""
		if completedAt.Valid {
			completedAtStr = completedAt.String
		}

		summary := map[string]interface{}{
			"job_id":             jobID,
			"total_db_count":     totalDBCount,
			"created_at":         createdAt,
			"state":              state,
			"completed_at":       completedAtStr,
			"total_size_kb":      totalSizeKB,
			"total_disk_size":    totalDiskSizeKB,
			"backup_mode":        backupMode,
			"total_full":         totalFull,
			"total_incremental":  totalIncremental,
			"total_failed":       totalFailed,
			"mysql_restart_time": mysqlRestartTime,
		}

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

// GetBackupSummaryByJobID gets a specific backup summary by job ID
func GetBackupSummaryByJobID(jobID string) (map[string]interface{}, error) {
	query := `SELECT job_id, total_db_count, created_at, state, completed_at, 
		total_size_kb, total_disk_size, backup_mode, total_full, total_incremental, total_failed, mysql_restart_time
		FROM backup_summary 
		WHERE job_id = ?`

	var jobIDResult, createdAt, state, backupMode string
	var totalDBCount, totalSizeKB, totalDiskSizeKB, totalFull, totalIncremental, totalFailed, mysqlRestartTime int
	var completedAt sql.NullString

	err := db.QueryRow(query, jobID).Scan(&jobIDResult, &totalDBCount, &createdAt, &state, &completedAt,
		&totalSizeKB, &totalDiskSizeKB, &backupMode, &totalFull, &totalIncremental, &totalFailed, &mysqlRestartTime)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found
		}
		return nil, err
	}

	// Convert sql.NullString to regular string
	completedAtStr := ""
	if completedAt.Valid {
		completedAtStr = completedAt.String
	}

	summary := map[string]interface{}{
		"job_id":             jobIDResult,
		"total_db_count":     totalDBCount,
		"created_at":         createdAt,
		"state":              state,
		"completed_at":       completedAtStr,
		"total_size_kb":      totalSizeKB,
		"total_disk_size":    totalDiskSizeKB,
		"backup_mode":        backupMode,
		"total_full":         totalFull,
		"total_incremental":  totalIncremental,
		"total_failed":       totalFailed,
		"mysql_restart_time": mysqlRestartTime,
	}

	return summary, nil
}

// GetRunningJobsWithSummary returns running jobs with summary data for the running jobs card
func GetRunningJobsWithSummary() (map[string]interface{}, error) {
	// Get running summaries (state = 'running') AND recent completed summaries
	summaryQuery := `SELECT job_id, total_db_count, created_at, state, completed_at, 
		total_size_kb, total_disk_size, backup_mode, total_full, total_incremental, total_failed, mysql_restart_time
		FROM backup_summary 
		WHERE state = 'running' OR (state = 'completed' AND completed_at >= datetime('now', '-1 day'))
		ORDER BY 
			CASE 
				WHEN state = 'running' THEN 1
				WHEN state = 'completed' THEN 2
				ELSE 3
			END,
			CASE 
				WHEN state = 'running' THEN created_at
				WHEN state = 'completed' THEN completed_at
				ELSE created_at
			END DESC
		LIMIT 2`

	summaryRows, err := db.Query(summaryQuery)
	if err != nil {
		return nil, err
	}
	defer summaryRows.Close()

	var summaries []map[string]interface{}
	for summaryRows.Next() {
		var jobID, createdAt, state, backupMode string
		var totalDBCount, totalSizeKB, totalDiskSizeKB, totalFull, totalIncremental, totalFailed, mysqlRestartTime int
		var completedAt sql.NullString // Use sql.NullString for nullable column

		err := summaryRows.Scan(&jobID, &totalDBCount, &createdAt, &state, &completedAt,
			&totalSizeKB, &totalDiskSizeKB, &backupMode, &totalFull, &totalIncremental, &totalFailed, &mysqlRestartTime)
		if err != nil {
			return nil, err
		}

		// Convert sql.NullString to regular string
		completedAtStr := ""
		if completedAt.Valid {
			completedAtStr = completedAt.String
		}

		summary := map[string]interface{}{
			"job_id":             jobID,
			"total_db_count":     totalDBCount,
			"created_at":         createdAt,
			"state":              state,
			"completed_at":       completedAtStr,
			"total_size_kb":      totalSizeKB,
			"total_disk_size":    totalDiskSizeKB,
			"backup_mode":        backupMode,
			"total_full":         totalFull,
			"total_incremental":  totalIncremental,
			"total_failed":       totalFailed,
			"mysql_restart_time": mysqlRestartTime,
		}

		summaries = append(summaries, summary)
	}

	// Get individual jobs for running summaries
	var allJobs []map[string]interface{}
	for _, summary := range summaries {
		jobID := summary["job_id"].(string)

		jobQuery := `SELECT id, job_id, database_name, backup_type, status, progress, 
			started_at, completed_at, estimated_size_kb, actual_size_kb, backup_file_path, error_message
			FROM backup_jobs 
			WHERE job_id = ? AND (
				status IN ('running', 'optimizing') OR
				id IN (
					SELECT id FROM backup_jobs 
					WHERE job_id = ? AND status IN ('done', 'failed')
					ORDER BY 
						CASE 
							WHEN status = 'done' THEN completed_at
							ELSE started_at
						END DESC
					LIMIT 5
				)
			)
			ORDER BY 
				CASE 
					WHEN status = 'running' THEN 1
					WHEN status = 'optimizing' THEN 2
					WHEN status = 'done' THEN 3
					WHEN status = 'failed' THEN 4
					ELSE 5
				END,
				CASE 
					WHEN status = 'running' THEN started_at
					WHEN status = 'optimizing' THEN started_at
					WHEN status = 'done' THEN completed_at
					ELSE started_at
				END DESC`

		jobRows, err := db.Query(jobQuery, jobID, jobID)
		if err != nil {
			continue
		}

		for jobRows.Next() {
			var id int
			var jobID, databaseName, backupType, status string
			var progress, estimatedSizeKB, actualSizeKB sql.NullInt64
			var startedAt, completedAt, backupFilePath, errorMessage sql.NullString

			err := jobRows.Scan(&id, &jobID, &databaseName, &backupType, &status, &progress,
				&startedAt, &completedAt, &estimatedSizeKB, &actualSizeKB, &backupFilePath, &errorMessage)
			if err != nil {
				continue
			}

			// Convert sql.NullString to regular string
			startedAtStr := ""
			if startedAt.Valid {
				startedAtStr = startedAt.String
			}
			completedAtStr := ""
			if completedAt.Valid {
				completedAtStr = completedAt.String
			}
			backupFilePathStr := ""
			if backupFilePath.Valid {
				backupFilePathStr = backupFilePath.String
			}
			errorMessageStr := ""
			if errorMessage.Valid {
				errorMessageStr = errorMessage.String
			}

			job := map[string]interface{}{
				"id":                id,
				"job_id":            jobID,
				"database_name":     databaseName,
				"backup_type":       backupType,
				"status":            status,
				"progress":          progress.Int64,
				"started_at":        startedAtStr,
				"completed_at":      completedAtStr,
				"estimated_size_kb": estimatedSizeKB.Int64,
				"actual_size_kb":    actualSizeKB.Int64,
				"backup_file_path":  backupFilePathStr,
				"error_message":     errorMessageStr,
			}

			allJobs = append(allJobs, job)
		}
		jobRows.Close()
	}

	// Calculate totals
	totalRunning := len(summaries)
	totalJobs := len(allJobs)

	// Count completed jobs
	completedJobs := 0
	for _, job := range allJobs {
		if job["status"] == "done" {
			completedJobs++
		}
	}

	return map[string]interface{}{
		"summaries":      summaries,
		"jobs":           allJobs,
		"total_running":  totalRunning,
		"total_jobs":     totalJobs,
		"completed_jobs": completedJobs,
	}, nil
}

// GetRecentActivityWithPagination returns paginated recent activity for dashboard
func GetRecentActivityWithPagination(page, limit int) (map[string]interface{}, error) {
	// Calculate offset
	offset := (page - 1) * limit

	// Get all summaries (not just recent ones) with pagination
	summaryQuery := `SELECT job_id, total_db_count, created_at, state, completed_at, 
		total_size_kb, total_disk_size, backup_mode, total_full, total_incremental, total_failed, mysql_restart_time
		FROM backup_summary 
		ORDER BY 
			CASE 
				WHEN state = 'running' THEN 1
				WHEN state = 'completed' THEN 2
				ELSE 3
			END,
			CASE 
				WHEN state = 'running' THEN created_at
				WHEN state = 'completed' THEN completed_at
				ELSE created_at
			END DESC
		LIMIT ? OFFSET ?`

	summaryRows, err := db.Query(summaryQuery, limit, offset)
	if err != nil {
		return nil, err
	}
	defer summaryRows.Close()

	var summaries []map[string]interface{}
	for summaryRows.Next() {
		var jobID, createdAt, state, backupMode string
		var totalDBCount, totalSizeKB, totalDiskSizeKB, totalFull, totalIncremental, totalFailed, mysqlRestartTime int
		var completedAt sql.NullString

		err := summaryRows.Scan(&jobID, &totalDBCount, &createdAt, &state, &completedAt,
			&totalSizeKB, &totalDiskSizeKB, &backupMode, &totalFull, &totalIncremental, &totalFailed, &mysqlRestartTime)
		if err != nil {
			return nil, err
		}

		// Convert sql.NullString to regular string
		completedAtStr := ""
		if completedAt.Valid {
			completedAtStr = completedAt.String
		}

		summary := map[string]interface{}{
			"job_id":             jobID,
			"total_db_count":     totalDBCount,
			"created_at":         createdAt,
			"state":              state,
			"completed_at":       completedAtStr,
			"total_size_kb":      totalSizeKB,
			"total_disk_size":    totalDiskSizeKB,
			"backup_mode":        backupMode,
			"total_full":         totalFull,
			"total_incremental":  totalIncremental,
			"total_failed":       totalFailed,
			"mysql_restart_time": mysqlRestartTime,
		}

		summaries = append(summaries, summary)
	}

	// Get total count for pagination
	countQuery := `SELECT COUNT(*) FROM backup_summary`
	var totalCount int
	err = db.QueryRow(countQuery).Scan(&totalCount)
	if err != nil {
		return nil, err
	}

	// Calculate pagination info
	totalPages := (totalCount + limit - 1) / limit
	hasNext := page < totalPages
	hasPrev := page > 1

	return map[string]interface{}{
		"summaries": summaries,
		"pagination": map[string]interface{}{
			"current_page": page,
			"total_pages":  totalPages,
			"total_count":  totalCount,
			"limit":        limit,
			"has_next":     hasNext,
			"has_prev":     hasPrev,
		},
	}, nil
}

func DeleteBackupJob(jobID string) error {
	query := `DELETE FROM backup_jobs WHERE job_id = ?`
	_, err := db.Exec(query, jobID)
	return err
}

// ClearAllBackupHistory clears all records from backup_summary and backup_jobs tables
func ClearAllBackupHistory() error {
	// Start a transaction to ensure both operations succeed or fail together
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Clear backup_jobs table
	_, err = tx.Exec(`DELETE FROM backup_jobs`)
	if err != nil {
		return fmt.Errorf("failed to clear backup_jobs: %v", err)
	}

	// Clear backup_summary table
	_, err = tx.Exec(`DELETE FROM backup_summary`)
	if err != nil {
		return fmt.Errorf("failed to clear backup_summary: %v", err)
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
}

// GetBackupJobByID returns a single backup job by ID
func GetBackupJobByID(jobID string) (map[string]interface{}, error) {
	query := `SELECT id, job_id, database_name, backup_type, status, progress, 
		started_at, completed_at, estimated_size_kb, actual_size_kb, backup_file_path, error_message
		FROM backup_jobs 
		WHERE id = ?`

	var id int
	var jobIDStr, databaseName, backupType, status string
	var progress, estimatedSizeKB, actualSizeKB sql.NullInt64
	var startedAt, completedAt, backupFilePath, errorMessage sql.NullString

	err := db.QueryRow(query, jobID).Scan(&id, &jobIDStr, &databaseName, &backupType, &status, &progress,
		&startedAt, &completedAt, &estimatedSizeKB, &actualSizeKB, &backupFilePath, &errorMessage)
	if err != nil {
		return nil, err
	}

	// Convert sql.NullString to regular string
	startedAtStr := ""
	if startedAt.Valid {
		startedAtStr = startedAt.String
	}
	completedAtStr := ""
	if completedAt.Valid {
		completedAtStr = completedAt.String
	}
	backupFilePathStr := ""
	if backupFilePath.Valid {
		backupFilePathStr = backupFilePath.String
	}
	errorMessageStr := ""
	if errorMessage.Valid {
		errorMessageStr = errorMessage.String
	}

	job := map[string]interface{}{
		"id":                id,
		"job_id":            jobIDStr,
		"database_name":     databaseName,
		"backup_type":       backupType,
		"status":            status,
		"progress":          progress.Int64,
		"started_at":        startedAtStr,
		"completed_at":      completedAtStr,
		"estimated_size_kb": estimatedSizeKB.Int64,
		"actual_size_kb":    actualSizeKB.Int64,
		"backup_file_path":  backupFilePathStr,
		"error_message":     errorMessageStr,
	}

	return job, nil
}

// GetFailedBackupJobsByJobID returns all failed backup jobs for a specific job_id
func GetFailedBackupJobsByJobID(jobID string) ([]map[string]interface{}, error) {
	query := `SELECT id, job_id, database_name, backup_type, status, progress, 
		started_at, completed_at, estimated_size_kb, actual_size_kb, backup_file_path, error_message
		FROM backup_jobs 
		WHERE job_id = ? AND status = 'failed'
		ORDER BY database_name`

	rows, err := db.Query(query, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []map[string]interface{}
	for rows.Next() {
		var id int
		var jobIDStr, databaseName, backupType, status string
		var progress, estimatedSizeKB, actualSizeKB sql.NullInt64
		var startedAt, completedAt, backupFilePath, errorMessage sql.NullString

		err := rows.Scan(&id, &jobIDStr, &databaseName, &backupType, &status, &progress,
			&startedAt, &completedAt, &estimatedSizeKB, &actualSizeKB, &backupFilePath, &errorMessage)
		if err != nil {
			return nil, err
		}

		// Convert sql.NullString to regular string
		startedAtStr := ""
		if startedAt.Valid {
			startedAtStr = startedAt.String
		}
		completedAtStr := ""
		if completedAt.Valid {
			completedAtStr = completedAt.String
		}
		backupFilePathStr := ""
		if backupFilePath.Valid {
			backupFilePathStr = backupFilePath.String
		}
		errorMessageStr := ""
		if errorMessage.Valid {
			errorMessageStr = errorMessage.String
		}

		job := map[string]interface{}{
			"id":                id,
			"job_id":            jobIDStr,
			"database_name":     databaseName,
			"backup_type":       backupType,
			"status":            status,
			"progress":          progress.Int64,
			"started_at":        startedAtStr,
			"completed_at":      completedAtStr,
			"estimated_size_kb": estimatedSizeKB.Int64,
			"actual_size_kb":    actualSizeKB.Int64,
			"backup_file_path":  backupFilePathStr,
			"error_message":     errorMessageStr,
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// ResetFailedBackupJobs resets failed backup jobs back to running status for retry
func ResetFailedBackupJobs(jobID string) error {
	query := `UPDATE backup_jobs 
		SET status = 'running', 
			progress = 0, 
			started_at = CURRENT_TIMESTAMP, 
			completed_at = NULL, 
			error_message = NULL,
			actual_size_kb = NULL,
			backup_file_path = NULL
		WHERE job_id = ? AND status = 'failed'`

	_, err := db.Exec(query, jobID)
	return err
}

// ResetBackupSummaryToRunning resets backup summary state back to running and updates failed count
func ResetBackupSummaryToRunning(jobID string, failedCount int) error {
	query := `UPDATE backup_summary 
		SET state = 'running', 
			completed_at = NULL,
			total_failed = ?
		WHERE job_id = ?`

	_, err := db.Exec(query, failedCount, jobID)
	return err
}

// GetBackupHistory returns paginated backup history with search and filtering
func GetBackupHistory(page, limit int, search, status, date, sort, jobId string) ([]map[string]interface{}, int, error) {
	offset := (page - 1) * limit

	// Build WHERE clause
	var whereConditions []string
	var args []interface{}

	if search != "" {
		whereConditions = append(whereConditions, "(id LIKE ? OR job_id LIKE ? OR database_name LIKE ? OR backup_file_path LIKE ?)")
		searchPattern := "%" + search + "%"
		args = append(args, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	if status != "" && status != "all" {
		whereConditions = append(whereConditions, "status = ?")
		args = append(args, status)
	}

	if date != "" {
		whereConditions = append(whereConditions, "DATE(started_at) = ?")
		args = append(args, date)
	}

	if jobId != "" {
		whereConditions = append(whereConditions, "job_id = ?")
		args = append(args, jobId)
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Count total records
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM backup_jobs %s", whereClause)
	var totalCount int
	err := db.QueryRow(countQuery, args...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Build ORDER BY clause
	orderBy := "started_at DESC" // default
	if sort != "" {
		switch sort {
		case "started_at_asc":
			orderBy = "started_at ASC"
		case "started_at_desc":
			orderBy = "started_at DESC"
		case "database_name_asc":
			orderBy = "database_name ASC"
		case "database_name_desc":
			orderBy = "database_name DESC"
		case "status_asc":
			orderBy = "status ASC"
		case "status_desc":
			orderBy = "status DESC"
		case "duration_asc":
			orderBy = "CASE WHEN completed_at IS NOT NULL AND completed_at != '' THEN (julianday(completed_at) - julianday(started_at)) ELSE 999999 END ASC, started_at DESC"
		case "duration_desc":
			orderBy = "CASE WHEN completed_at IS NOT NULL AND completed_at != '' THEN (julianday(completed_at) - julianday(started_at)) ELSE 0 END DESC, started_at DESC"
		case "estimated_size_kb_asc":
			orderBy = "estimated_size_kb ASC"
		case "estimated_size_kb_desc":
			orderBy = "estimated_size_kb DESC"
		case "actual_size_kb_asc":
			orderBy = "actual_size_kb ASC"
		case "actual_size_kb_desc":
			orderBy = "actual_size_kb DESC"
		}
	}

	// Get paginated records
	query := fmt.Sprintf(`SELECT id, job_id, database_name, backup_type, status, progress, 
		started_at, completed_at, estimated_size_kb, actual_size_kb, backup_file_path, error_message
		FROM backup_jobs 
		%s
		ORDER BY %s
		LIMIT ? OFFSET ?`, whereClause, orderBy)

	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var jobs []map[string]interface{}
	for rows.Next() {
		var id int
		var jobID, databaseName, backupType, status string
		var progress, estimatedSizeKB, actualSizeKB sql.NullInt64
		var startedAt, completedAt, backupFilePath, errorMessage sql.NullString

		err := rows.Scan(&id, &jobID, &databaseName, &backupType, &status, &progress,
			&startedAt, &completedAt, &estimatedSizeKB, &actualSizeKB, &backupFilePath, &errorMessage)
		if err != nil {
			return nil, 0, err
		}

		// Convert sql.NullString to regular string
		startedAtStr := ""
		if startedAt.Valid {
			startedAtStr = startedAt.String
		}
		completedAtStr := ""
		if completedAt.Valid {
			completedAtStr = completedAt.String
		}
		backupFilePathStr := ""
		if backupFilePath.Valid {
			backupFilePathStr = backupFilePath.String
		}
		errorMessageStr := ""
		if errorMessage.Valid {
			errorMessageStr = errorMessage.String
		}

		// Calculate duration
		duration := ""
		if status == "done" && completedAtStr != "" && startedAtStr != "" {
			startTime, err1 := time.Parse("2006-01-02 15:04:05", startedAtStr)
			endTime, err2 := time.Parse("2006-01-02 15:04:05", completedAtStr)
			if err1 == nil && err2 == nil {
				duration = formatDuration(endTime.Sub(startTime))
			}
		} else if status != "done" {
			duration = "In Progress"
		}

		job := map[string]interface{}{
			"id":                id,
			"job_id":            jobID,
			"database_name":     databaseName,
			"backup_type":       backupType,
			"status":            status,
			"progress":          progress.Int64,
			"started_at":        startedAtStr,
			"completed_at":      completedAtStr,
			"estimated_size_kb": estimatedSizeKB.Int64,
			"actual_size_kb":    actualSizeKB.Int64,
			"backup_file_path":  backupFilePathStr,
			"error_message":     errorMessageStr,
			"duration":          duration,
		}

		jobs = append(jobs, job)
	}

	return jobs, totalCount, nil
}

// GetDatabaseBackupGroups returns backup history for a specific database grouped by full/incremental relationships
func GetDatabaseBackupGroups(databaseName string) ([]map[string]interface{}, error) {
	// Get all completed backups for the database, ordered by start time (oldest first for proper grouping)
	query := `SELECT id, job_id, database_name, backup_type, status, progress, 
		started_at, completed_at, estimated_size_kb, actual_size_kb, backup_file_path, error_message
		FROM backup_jobs 
		WHERE database_name = ? AND status = 'done'
		ORDER BY started_at ASC`

	rows, err := db.Query(query, databaseName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allJobs []map[string]interface{}
	for rows.Next() {
		var id int
		var jobID, databaseName, backupType, status string
		var progress, estimatedSizeKB, actualSizeKB sql.NullInt64
		var startedAt, completedAt, backupFilePath, errorMessage sql.NullString

		err := rows.Scan(&id, &jobID, &databaseName, &backupType, &status, &progress,
			&startedAt, &completedAt, &estimatedSizeKB, &actualSizeKB, &backupFilePath, &errorMessage)
		if err != nil {
			return nil, err
		}

		// Convert sql.NullString to regular string
		startedAtStr := ""
		if startedAt.Valid {
			startedAtStr = startedAt.String
		}
		completedAtStr := ""
		if completedAt.Valid {
			completedAtStr = completedAt.String
		}
		backupFilePathStr := ""
		if backupFilePath.Valid {
			backupFilePathStr = backupFilePath.String
		}
		errorMessageStr := ""
		if errorMessage.Valid {
			errorMessageStr = errorMessage.String
		}

		job := map[string]interface{}{
			"id":                id,
			"job_id":            jobID,
			"database_name":     databaseName,
			"backup_type":       backupType,
			"status":            status,
			"progress":          progress.Int64,
			"started_at":        startedAtStr,
			"completed_at":      completedAtStr,
			"estimated_size_kb": estimatedSizeKB.Int64,
			"actual_size_kb":    actualSizeKB.Int64,
			"backup_file_path":  backupFilePathStr,
			"error_message":     errorMessageStr,
		}

		allJobs = append(allJobs, job)
	}

	// Group backups by full/incremental relationships
	LogInfo("Processing %d backup jobs for database %s", len(allJobs), databaseName)

	var groups []map[string]interface{}
	var currentGroup []map[string]interface{}

	for i, job := range allJobs {
		backupType := job["backup_type"].(string)
		LogInfo("Job %d: Type=%s, ID=%v, Started=%v", i+1, backupType, job["id"], job["started_at"])

		// Check if this is a full backup
		if strings.Contains(backupType, "full") {
			// If we have a current group, save it
			if len(currentGroup) > 0 {
				LogInfo("Creating group with %d backups (1 full + %d incremental)", len(currentGroup), len(currentGroup)-1)
				groups = append(groups, map[string]interface{}{
					"group_type":          "full_group",
					"full_backup":         currentGroup[0],  // First item is always the full backup
					"incremental_backups": currentGroup[1:], // Rest are incremental
					"total_backups":       len(currentGroup),
					"group_start_time":    currentGroup[0]["started_at"],
				})
			}
			// Start new group with this full backup
			LogInfo("Starting new group with full backup ID=%v", job["id"])
			currentGroup = []map[string]interface{}{job}
		} else if strings.Contains(backupType, "inc") {
			// Add incremental backup to current group
			currentGroup = append(currentGroup, job)
		}
	}

	// Don't forget the last group
	if len(currentGroup) > 0 {
		LogInfo("Creating final group with %d backups (1 full + %d incremental)", len(currentGroup), len(currentGroup)-1)
		groups = append(groups, map[string]interface{}{
			"group_type":          "full_group",
			"full_backup":         currentGroup[0],  // First item is always the full backup
			"incremental_backups": currentGroup[1:], // Rest are incremental
			"total_backups":       len(currentGroup),
			"group_start_time":    currentGroup[0]["started_at"],
		})
	}

	LogInfo("Created %d backup groups for database %s", len(groups), databaseName)
	return groups, nil
}

// GetDatabaseBackupFiles returns backup files for a specific database from filesystem with pagination
func GetDatabaseBackupFiles(databaseName string, config *Config, page, limit int) ([]map[string]interface{}, int, error) {
	backupDir := filepath.Join(config.Backup.BackupDir, databaseName)

	// Check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []map[string]interface{}{}, 0, nil // Return empty array, not error
	}

	var allFiles []map[string]interface{}

	// Look for full backup files
	fullPattern := fmt.Sprintf("full_%s_*.sql", databaseName)
	fullMatches, err := filepath.Glob(filepath.Join(backupDir, fullPattern))
	if err != nil {
		LogWarn("Error searching for full backup files for %s: %v", databaseName, err)
	} else {
		for _, match := range fullMatches {
			fileInfo, err := os.Stat(match)
			if err != nil {
				continue
			}

			fileName := filepath.Base(match)
			fileSize := fileInfo.Size()

			// Parse timestamp from filename
			timestamp := parseTimestampFromFilename(fileName)

			allFiles = append(allFiles, map[string]interface{}{
				"file_path":   match,
				"file_name":   fileName,
				"file_size":   fileSize,
				"backup_type": "full",
				"timestamp":   timestamp,
				"modified_at": fileInfo.ModTime(),
			})
		}
	}

	// Look for full .gz files
	fullGzPattern := fmt.Sprintf("full_%s_*.gz", databaseName)
	fullGzMatches, err := filepath.Glob(filepath.Join(backupDir, fullGzPattern))
	if err != nil {
		LogWarn("Error searching for compressed full backup files for %s: %v", databaseName, err)
	} else {
		for _, match := range fullGzMatches {
			fileInfo, err := os.Stat(match)
			if err != nil {
				continue
			}

			fileName := filepath.Base(match)
			fileSize := fileInfo.Size()

			// Parse timestamp from filename
			timestamp := parseTimestampFromFilename(fileName)

			allFiles = append(allFiles, map[string]interface{}{
				"file_path":   match,
				"file_name":   fileName,
				"file_size":   fileSize,
				"backup_type": "full",
				"timestamp":   timestamp,
				"modified_at": fileInfo.ModTime(),
			})
		}
	}

	// Look for incremental backup files
	incPattern := fmt.Sprintf("inc_%s_*.sql", databaseName)
	incMatches, err := filepath.Glob(filepath.Join(backupDir, incPattern))
	if err != nil {
		LogWarn("Error searching for incremental backup files for %s: %v", databaseName, err)
	} else {
		for _, match := range incMatches {
			fileInfo, err := os.Stat(match)
			if err != nil {
				continue
			}

			fileName := filepath.Base(match)
			fileSize := fileInfo.Size()

			// Parse timestamp from filename
			timestamp := parseTimestampFromFilename(fileName)

			allFiles = append(allFiles, map[string]interface{}{
				"file_path":   match,
				"file_name":   fileName,
				"file_size":   fileSize,
				"backup_type": "incremental",
				"timestamp":   timestamp,
				"modified_at": fileInfo.ModTime(),
			})
		}
	}

	// Look for incremental .gz files
	incGzPattern := fmt.Sprintf("inc_%s_*.gz", databaseName)
	incGzMatches, err := filepath.Glob(filepath.Join(backupDir, incGzPattern))
	if err != nil {
		LogWarn("Error searching for compressed incremental backup files for %s: %v", databaseName, err)
	} else {
		for _, match := range incGzMatches {
			fileInfo, err := os.Stat(match)
			if err != nil {
				continue
			}

			fileName := filepath.Base(match)
			fileSize := fileInfo.Size()

			// Parse timestamp from filename
			timestamp := parseTimestampFromFilename(fileName)

			allFiles = append(allFiles, map[string]interface{}{
				"file_path":   match,
				"file_name":   fileName,
				"file_size":   fileSize,
				"backup_type": "incremental",
				"timestamp":   timestamp,
				"modified_at": fileInfo.ModTime(),
			})
		}
	}

	// Sort files by timestamp (oldest first)
	sort.Slice(allFiles, func(i, j int) bool {
		timeI := allFiles[i]["timestamp"].(time.Time)
		timeJ := allFiles[j]["timestamp"].(time.Time)
		return timeI.Before(timeJ)
	})

	// Group files by full/incremental relationships
	var groups []map[string]interface{}
	var currentGroup []map[string]interface{}

	for _, file := range allFiles {
		backupType := file["backup_type"].(string)

		if backupType == "full" {
			// If we have a current group, save it
			if len(currentGroup) > 0 {
				groups = append(groups, map[string]interface{}{
					"group_type":          "full_group",
					"full_backup":         currentGroup[0],  // First item is always the full backup
					"incremental_backups": currentGroup[1:], // Rest are incremental
					"total_backups":       len(currentGroup),
					"group_start_time":    currentGroup[0]["timestamp"],
				})
			}
			// Start new group with this full backup
			currentGroup = []map[string]interface{}{file}
		} else if backupType == "incremental" {
			// Add incremental backup to current group
			currentGroup = append(currentGroup, file)
		}
	}

	// Don't forget the last group
	if len(currentGroup) > 0 {
		LogInfo("Creating final group with %d backups (1 full + %d incremental)", len(currentGroup), len(currentGroup)-1)
		groups = append(groups, map[string]interface{}{
			"group_type":          "full_group",
			"full_backup":         currentGroup[0],  // First item is always the full backup
			"incremental_backups": currentGroup[1:], // Rest are incremental
			"total_backups":       len(currentGroup),
			"group_start_time":    currentGroup[0]["timestamp"],
		})
	}

	// Sort groups by newest full backup first (descending by timestamp)
	sort.Slice(groups, func(i, j int) bool {
		timeI := groups[i]["group_start_time"].(time.Time)
		timeJ := groups[j]["group_start_time"].(time.Time)
		return timeI.After(timeJ) // Newest first
	})

	// Sort incremental backups within each group by oldest first (ascending by timestamp)
	for _, group := range groups {
		incrementalBackups := group["incremental_backups"].([]map[string]interface{})
		sort.Slice(incrementalBackups, func(i, j int) bool {
			timeI := incrementalBackups[i]["timestamp"].(time.Time)
			timeJ := incrementalBackups[j]["timestamp"].(time.Time)
			return timeI.Before(timeJ) // Oldest first
		})
		group["incremental_backups"] = incrementalBackups
	}

	totalGroups := len(groups)

	// Apply pagination
	start := (page - 1) * limit
	end := start + limit

	if start >= totalGroups {
		return []map[string]interface{}{}, totalGroups, nil
	}

	if end > totalGroups {
		end = totalGroups
	}

	paginatedGroups := groups[start:end]

	LogInfo("Created %d backup groups for database %s (showing %d-%d of %d)", len(paginatedGroups), databaseName, start+1, end, totalGroups)
	return paginatedGroups, totalGroups, nil
}

// parseTimestampFromFilename extracts timestamp from backup filename
func parseTimestampFromFilename(fileName string) time.Time {
	// Remove extension
	nameWithoutExt := strings.TrimSuffix(fileName, ".sql")
	nameWithoutExt = strings.TrimSuffix(nameWithoutExt, ".gz")

	parts := strings.Split(nameWithoutExt, "_")
	if len(parts) >= 3 {
		// For format: full_dbname_20251001_224620.950678 or inc_dbname_20251001_224620.950678
		// We need the last two parts: 20251001 and 224620.950678
		timestampStr := parts[len(parts)-2] + "_" + parts[len(parts)-1]

		// Try parsing with microseconds first
		backupTime, err := time.Parse("20060102_150405.000000", timestampStr)
		if err != nil {
			// Fallback to format without microseconds
			backupTime, err = time.Parse("20060102_150405", timestampStr)
			if err != nil {
				LogWarn("Failed to parse timestamp from %s: %v", fileName, err)
				return time.Time{}
			}
		}
		return backupTime
	}
	return time.Time{}
}

// formatDuration formats a duration into human-readable format
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	} else if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
}

// GetBackupTimelineData returns timeline data for backup size and duration over specified days
func GetBackupTimelineData(days int) (map[string]interface{}, error) {
	// Calculate the start date
	startDate := time.Now().AddDate(0, 0, -days)
	startDateStr := startDate.Format("2006-01-02")

	// Query to get daily backup data
	query := `
		SELECT 
			DATE(started_at) as backup_date,
			COUNT(*) as backup_count,
			SUM(CASE WHEN status = 'done' THEN actual_size_kb ELSE 0 END) as total_size_kb,
			AVG(CASE WHEN status = 'done' AND completed_at IS NOT NULL AND completed_at != '' 
				THEN (julianday(completed_at) - julianday(started_at)) * 24 * 60 
				ELSE NULL END) as avg_duration_minutes,
			SUM(CASE WHEN backup_type = 'full' THEN 1 ELSE 0 END) as full_backup_count,
			SUM(CASE WHEN backup_type = 'incremental' THEN 1 ELSE 0 END) as incremental_backup_count
		FROM backup_jobs 
		WHERE DATE(started_at) >= ? AND status = 'done'
		GROUP BY DATE(started_at)
		ORDER BY backup_date ASC
	`

	rows, err := db.Query(query, startDateStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var timelineData []map[string]interface{}
	var totalSizeKB int64
	var totalBackups int
	var totalDurationMinutes float64

	for rows.Next() {
		var backupDate string
		var backupCount int
		var totalSizeKBForDay int64
		var avgDurationMinutes sql.NullFloat64
		var fullBackupCount int
		var incrementalBackupCount int

		err := rows.Scan(&backupDate, &backupCount, &totalSizeKBForDay, &avgDurationMinutes, &fullBackupCount, &incrementalBackupCount)
		if err != nil {
			return nil, err
		}

		// Convert KB to GB for display
		sizeGB := float64(totalSizeKBForDay) / (1024 * 1024)

		// Handle null duration
		durationMinutes := 0.0
		if avgDurationMinutes.Valid {
			durationMinutes = avgDurationMinutes.Float64
		}

		dayData := map[string]interface{}{
			"date":                     backupDate,
			"backup_count":             backupCount,
			"size_gb":                  sizeGB,
			"duration_minutes":         durationMinutes,
			"full_backup_count":        fullBackupCount,
			"incremental_backup_count": incrementalBackupCount,
		}

		timelineData = append(timelineData, dayData)
		totalSizeKB += totalSizeKBForDay
		totalBackups += backupCount
		totalDurationMinutes += durationMinutes * float64(backupCount)
	}

	// Calculate summary statistics
	var avgSizeGB float64
	var avgDurationMinutes float64
	if totalBackups > 0 {
		avgSizeGB = float64(totalSizeKB) / (1024 * 1024) / float64(totalBackups)
		avgDurationMinutes = totalDurationMinutes / float64(totalBackups)
	}

	// Calculate growth rate (compare first half vs second half of period)
	var growthRate float64
	if len(timelineData) > 1 {
		midPoint := len(timelineData) / 2
		firstHalfSize := 0.0
		secondHalfSize := 0.0

		for i := 0; i < midPoint; i++ {
			firstHalfSize += timelineData[i]["size_gb"].(float64)
		}
		for i := midPoint; i < len(timelineData); i++ {
			secondHalfSize += timelineData[i]["size_gb"].(float64)
		}

		if firstHalfSize > 0 {
			growthRate = ((secondHalfSize - firstHalfSize) / firstHalfSize) * 100
		}
	}

	result := map[string]interface{}{
		"timeline_data": timelineData,
		"summary": map[string]interface{}{
			"total_backup_size_gb":     float64(totalSizeKB) / (1024 * 1024),
			"average_size_gb":          avgSizeGB,
			"average_duration_minutes": avgDurationMinutes,
			"growth_rate_percent":      growthRate,
			"total_backups":            totalBackups,
			"days_analyzed":            len(timelineData),
		},
	}

	return result, nil
}

// Helper function to generate job ID based on current timestamp
func GenerateJobID() string {
	now := time.Now()
	return fmt.Sprintf("%d", now.Unix()) // Unix timestamp format
}

// GetDatabaseMetrics returns current database operation metrics
func GetDatabaseMetrics() map[string]interface{} {
	dbMetrics.mutex.RLock()
	defer dbMetrics.mutex.RUnlock()

	uptime := time.Since(dbMetrics.LastResetTime)

	return map[string]interface{}{
		"total_operations":      dbMetrics.TotalOperations,
		"failed_operations":     dbMetrics.FailedOperations,
		"retry_operations":      dbMetrics.RetryOperations,
		"lock_contention_ops":   dbMetrics.LockContentionOps,
		"batch_operations":      dbMetrics.BatchOperations,
		"success_rate":          calculateSuccessRate(dbMetrics.TotalOperations, dbMetrics.FailedOperations),
		"lock_contention_rate":  calculateContentionRate(dbMetrics.TotalOperations, dbMetrics.LockContentionOps),
		"uptime_seconds":        uptime.Seconds(),
		"operations_per_second": float64(dbMetrics.TotalOperations) / uptime.Seconds(),
	}
}

// calculateSuccessRate calculates the success rate percentage
func calculateSuccessRate(total, failed int64) float64 {
	if total == 0 {
		return 100.0
	}
	return float64(total-failed) / float64(total) * 100.0
}

// calculateContentionRate calculates the lock contention rate percentage
func calculateContentionRate(total, contention int64) float64 {
	if total == 0 {
		return 0.0
	}
	return float64(contention) / float64(total) * 100.0
}

// ResetDatabaseMetrics resets the database metrics counters
func ResetDatabaseMetrics() {
	dbMetrics.mutex.Lock()
	defer dbMetrics.mutex.Unlock()

	dbMetrics.TotalOperations = 0
	dbMetrics.FailedOperations = 0
	dbMetrics.RetryOperations = 0
	dbMetrics.LockContentionOps = 0
	dbMetrics.BatchOperations = 0
	dbMetrics.LastResetTime = time.Now()

	LogInfo("Database metrics reset")
}
