package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/mem"
)

// BackupFullRequest represents a manual full backup request from web UI
type BackupFullRequest struct {
	JobID       string   `json:"job_id"`
	Databases   []string `json:"databases"`
	BackupMode  string   `json:"backup_mode"`
	RequestedBy string   `json:"requested_by"`
}

// BackupFullResponse represents the response after starting backup
type BackupFullResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	JobID   string `json:"job_id"`
	Details struct {
		BackupMode string   `json:"backup_mode"`
		Databases  []string `json:"databases"`
		TotalDBs   int      `json:"total_dbs"`
	} `json:"details"`
}

// StartFullBackup starts a manual full backup process
func StartFullBackup(request BackupFullRequest) BackupFullResponse {
	dbList := formatDatabaseList(request.Databases)
	LogInfo("🚀 [BACKUP-START] Starting manual full backup - JobID: %s, Databases: %s, Mode: %s, RequestedBy: %s",
		request.JobID, dbList, request.BackupMode, request.RequestedBy)

	// Reset global abort flag when starting new backup
	ResetGlobalBackupAbort()

	config, err := loadConfig("config.json")
	if err != nil {
		LogError("❌ [CONFIG-ERROR] Failed to load config for backup: %v", err)
		return BackupFullResponse{
			Success: false,
			Message: "Failed to load configuration",
		}
	}

	if len(request.Databases) == 0 {
		LogWarn("⚠️ [VALIDATION] No databases specified for backup")
		return BackupFullResponse{
			Success: false,
			Message: "No databases specified for backup",
		}
	}
	LogDebug("✅ [VALIDATION] Database list validated - Count: %d", len(request.Databases))

	err = CreateBackupSummary(request.JobID, request.BackupMode, len(request.Databases))
	if err != nil {
		LogError("❌ [SQLITE-ERROR] Failed to create backup summary: %v", err)
		return BackupFullResponse{
			Success: false,
			Message: "Failed to create backup summary in database",
		}
	}

	go executeFullBackup(request, config)

	response := BackupFullResponse{
		Success: true,
		Message: "Backup process started successfully",
		JobID:   request.JobID,
	}
	response.Details.BackupMode = request.BackupMode
	response.Details.Databases = request.Databases
	response.Details.TotalDBs = len(request.Databases)

	return response
}

// RetryFailedBackups retries all failed databases for an existing backup job
func RetryFailedBackups(jobID string) error {
	LogInfo("🔄 [RETRY] Starting retry for failed databases - JobID: %s", jobID)

	// Get the backup summary to determine backup mode
	summary, err := GetBackupSummaryByJobID(jobID)
	if err != nil {
		return fmt.Errorf("failed to get backup summary: %v", err)
	}
	if summary == nil {
		return fmt.Errorf("backup summary not found for job_id: %s", jobID)
	}

	// Check if summary is completed
	if summary["state"].(string) != "completed" {
		return fmt.Errorf("backup job %s is not completed, cannot retry", jobID)
	}

	// Get failed backup jobs
	failedJobs, err := GetFailedBackupJobsByJobID(jobID)
	if err != nil {
		return fmt.Errorf("failed to get failed backup jobs: %v", err)
	}
	if len(failedJobs) == 0 {
		return fmt.Errorf("no failed backup jobs found for job_id: %s", jobID)
	}

	LogInfo("🔄 [RETRY] Found %d failed databases to retry", len(failedJobs))

	// Get backup mode from summary
	backupMode := summary["backup_mode"].(string)

	// Reset failed jobs to running status
	err = ResetFailedBackupJobs(jobID)
	if err != nil {
		return fmt.Errorf("failed to reset backup jobs: %v", err)
	}

	// Reset backup summary state to running and update failed count
	err = ResetBackupSummaryToRunning(jobID, 0)
	if err != nil {
		return fmt.Errorf("failed to reset backup summary: %v", err)
	}

	// Extract database names from failed jobs
	var databases []string
	for _, job := range failedJobs {
		dbName := job["database_name"].(string)
		databases = append(databases, dbName)
	}

	// Load config
	retryConfig, err := loadConfig("config.json")
	if err != nil {
		return fmt.Errorf("failed to load config for retry: %v", err)
	}

	// Create backup request for retry
	retryRequest := BackupFullRequest{
		JobID:       jobID,
		Databases:   databases,
		BackupMode:  backupMode,
		RequestedBy: "retry",
	}

	// Execute retry backup in a goroutine
	go executeFullBackup(retryRequest, retryConfig)

	LogInfo("🔄 [RETRY] Retry started for %d failed databases", len(databases))
	return nil
}

func executeFullBackup(request BackupFullRequest, config *Config) {
	totalSizeKB := 0
	totalDiskSizeKB := 0
	totalFull := 0
	totalFailed := 0
	totalMySQLRestartTime := int64(0) // Total MySQL restart time in seconds

	// For retries, get existing summary values to accumulate on top of them
	var existingIncremental int
	if request.RequestedBy == "retry" {
		existingSummary, err := GetBackupSummaryByJobID(request.JobID)
		if err == nil && existingSummary != nil {
			// Use existing values as starting point
			if val, ok := existingSummary["total_size_kb"].(int); ok {
				totalSizeKB = val
			}
			if val, ok := existingSummary["total_disk_size"].(int); ok {
				totalDiskSizeKB = val
			}
			if val, ok := existingSummary["total_full"].(int); ok {
				totalFull = val
			}
			if val, ok := existingSummary["total_incremental"].(int); ok {
				existingIncremental = val
			}
			// Don't use existing total_failed as we reset it to 0 when retrying
			LogInfo("🔄 [RETRY] Starting with existing totals - Size: %d KB, Disk: %d KB, Full: %d, Incremental: %d",
				totalSizeKB, totalDiskSizeKB, totalFull, existingIncremental)
		}
	}

	backupType := "force-full"
	if request.BackupMode == "auto" {
		backupType = "auto-full"
	}

	// Create shared MySQL connection pool for all workers
	// Limit pool to Parallel Processes * 3 (each worker may use 3-4 connections)
	dsn, err := buildMySQLDSN(config)
	if err != nil {
		LogError("❌ [CONNECTION-POOL] Failed to build DSN: %v", err)
		return
	}

	mysqlPool, err := sql.Open("mysql", dsn)
	if err != nil {
		LogError("❌ [CONNECTION-POOL] Failed to create MySQL connection pool: %v", err)
		return
	}
	defer mysqlPool.Close()

	// Configure connection pool to limit connections to Parallel * 3
	maxConns := config.Backup.Parallel * 3
	mysqlPool.SetMaxOpenConns(maxConns)
	mysqlPool.SetMaxIdleConns(config.Backup.Parallel)
	mysqlPool.SetConnMaxLifetime(30 * time.Minute)

	// Test the connection pool
	if err := mysqlPool.Ping(); err != nil {
		LogError("❌ [CONNECTION-POOL] Failed to ping MySQL connection pool: %v", err)
		mysqlPool.Close()
		return
	}

	LogInfo("✅ [CONNECTION-POOL] MySQL connection pool created - MaxOpen: %d, MaxIdle: %d", maxConns, config.Backup.Parallel)

	processQueue := make(chan string, len(request.Databases))
	activeProcesses := make(chan struct{}, config.Backup.Parallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var abortBackup bool
	var mysqlRestartInProgress bool

	for _, dbName := range request.Databases {
		processQueue <- dbName
	}
	close(processQueue)

	// Start worker goroutines
	LogInfo("👥 [WORKERS] Starting %d worker goroutines for parallel processing", config.Backup.Parallel)
	for i := 0; i < config.Backup.Parallel; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for dbName := range processQueue {
				// Check for global abort signal first
				if CheckGlobalBackupAbort() {
					LogWarn("⚠️ [WORKER-%d] Backup aborted due to global stop signal", workerID)
					return
				}

				mu.Lock()
				if abortBackup {
					mu.Unlock()
					LogWarn("⚠️ [WORKER-%d] Backup aborted due to MySQL service failure", workerID)

					// Update job status to failed due to abort
					updateErr := CompleteBackupJob(request.JobID, dbName, false, 0, "", "Backup aborted due to MySQL service failure")
					if updateErr != nil {
						LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
					}

					return
				}
				mu.Unlock()

				// Check memory threshold before acquiring process slot (Linux only)
				if runtime.GOOS == "linux" && shouldRestartMySQL(config) {
					LogWarn("⚠️ [MEMORY] Memory threshold exceeded (%.2f%% > %d%%), initiating MySQL restart before starting %s",
						getCurrentMemoryUsage(), config.Backup.MaxMemoryThreshold, dbName)

					// Check if MySQL restart is already in progress
					mu.Lock()
					if mysqlRestartInProgress {
						mu.Unlock()
						LogDebug("⏳ [WORKER-%d] MySQL restart already in progress, waiting...", workerID)
						for {
							time.Sleep(5 * time.Second)
							mu.Lock()
							if !mysqlRestartInProgress {
								mu.Unlock()
								break
							}
							mu.Unlock()
						}
						LogDebug("✅ [WORKER-%d] MySQL restart completed, proceeding with %s", workerID, dbName)
					} else {
						mysqlRestartInProgress = true
						mu.Unlock()

						LogDebug("⏳ [MEMORY] Waiting for all active backup processes to complete before MySQL restart...")
						for j := 0; j < config.Backup.Parallel; j++ {
							activeProcesses <- struct{}{}
						}
						for j := 0; j < config.Backup.Parallel; j++ {
							<-activeProcesses
						}
						LogDebug("✅ [MEMORY] All active processes completed, proceeding with MySQL restart")

						LogDebug("🔄 [MYSQL-RESTART] Starting MySQL service restart with monitoring")
						success, restartDuration := restartMySQLServiceWithMonitoring(config, &abortBackup)
						if !success {
							LogError("❌ [MYSQL-RESTART] MySQL service restart failed, aborting all backup processes")

							// Update job status to failed due to MySQL restart failure
							updateErr := CompleteBackupJob(request.JobID, dbName, false, 0, "", "MySQL service restart failed - backup aborted")
							if updateErr != nil {
								LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
							}

							mu.Lock()
							abortBackup = true
							mysqlRestartInProgress = false
							mu.Unlock()
							return
						}
						LogDebug("✅ [MYSQL-RESTART] MySQL service restart completed successfully")

						// Track MySQL restart time
						mu.Lock()
						totalMySQLRestartTime += int64(restartDuration.Seconds())
						mu.Unlock()

						mu.Lock()
						mysqlRestartInProgress = false
						mu.Unlock()

						LogDebug("✅ [MEMORY] Memory threshold cleared after MySQL restart, proceeding with %s", dbName)
					}
				}

				activeProcesses <- struct{}{}

				// Check for global abort before starting backup
				if CheckGlobalBackupAbort() {
					LogWarn("⚠️ [WORKER-%d] Backup aborted before starting %s", workerID, dbName)
					<-activeProcesses
					return
				}

				LogDebug("🚀 [WORKER-%d] Starting full backup for database: %s", workerID, dbName)

				estimatedSizeKB := 0
				if dbSizeBytes, err := getDatabaseSize(dbName, mysqlPool); err == nil {
					estimatedSizeKB = int(dbSizeBytes / 1024)
				} else {
					LogWarn("⚠️ [ESTIMATE] Failed to get estimated size for %s: %v", dbName, err)
				}

				// For retries, jobs already exist, so we only create if RequestedBy is not "retry"
				if request.RequestedBy != "retry" {
					err := CreateBackupJob(request.JobID, dbName, backupType, estimatedSizeKB)
					if err != nil {
						LogError("❌ [SQLITE-ERROR] Failed to create job for %s: %v", dbName, err)
					} else {
						LogDebug("✅ [SQLITE] Job record created for %s", dbName)
						go broadcastJobsUpdate()
					}
				} else {
					// For retries, update existing job status and estimate if needed
					LogDebug("🔄 [RETRY] Using existing job record for %s", dbName)
					go broadcastJobsUpdate()
				}

				mu.Lock()
				currentActiveProcesses := config.Backup.Parallel - len(activeProcesses)
				mu.Unlock()
				LogDebug("📊 [PARALLEL-STATUS] Worker-%d starting %s, %d/%d processes currently active",
					workerID, dbName, currentActiveProcesses, config.Backup.Parallel)

				LogDebug("✅ [MEMORY] Memory usage OK (%.2f%% <= %d%%), proceeding with backup",
					getCurrentMemoryUsage(), config.Backup.MaxMemoryThreshold)

				if config.Backup.OptimizeTables {
					// Check for global abort before optimization
					if CheckGlobalBackupAbort() {
						LogWarn("⚠️ [WORKER-%d] Backup aborted before optimization of %s", workerID, dbName)
						<-activeProcesses
						return
					}

					LogDebug("🔧 [OPTIMIZE] Worker-%d: Starting database optimization for %s", workerID, dbName)

					if err := UpdateBackupJobStatusByDB(request.JobID, dbName, "optimizing"); err != nil {
						LogError("❌ [SQLITE-ERROR] Failed to set status to optimizing for %s: %v", dbName, err)
					} else {
						go broadcastJobsUpdate()
					}

					optimizeResult := optimizeDatabaseWithProgress(dbName, request.JobID, config, mysqlPool)
					if !optimizeResult.Success {
						LogWarn("⚠️ [OPTIMIZE] Worker-%d: Database optimization failed for %s: %s", workerID, dbName, optimizeResult.ErrorMessage)

						// Store optimization error in database
						LogDebug("💾 [SQLITE] Storing optimization error for %s", dbName)
						if err := UpdateBackupJobError(request.JobID, dbName, fmt.Sprintf("Optimization failed: %s", optimizeResult.ErrorMessage)); err != nil {
							LogError("❌ [SQLITE-ERROR] Failed to store optimization error for %s: %v", dbName, err)
						}

						if err := UpdateBackupJobStatusByDB(request.JobID, dbName, "running"); err != nil {
							LogError("❌ [SQLITE-ERROR] Failed to set status back to running for %s: %v", dbName, err)
						} else {
							go broadcastJobsUpdate()
						}
					} else {
						LogDebug("✅ [OPTIMIZE] Worker-%d: Database optimization completed successfully for %s", workerID, dbName)
						if err := UpdateBackupJobStatusByDB(request.JobID, dbName, "running"); err != nil {
							LogError("❌ [SQLITE-ERROR] Failed to set status back to running for %s: %v", dbName, err)
						} else {
							go broadcastJobsUpdate()
						}
					}
				}

				// Check for global abort before starting actual backup
				if CheckGlobalBackupAbort() {
					LogWarn("⚠️ [WORKER-%d] Backup aborted before starting actual backup of %s", workerID, dbName)
					<-activeProcesses
					return
				}

				LogDebug("💾 [BACKUP] Worker-%d: Starting mysqldump backup for database: %s", workerID, dbName)
				backupResult := executeDatabaseBackup(dbName, request.JobID, config, mysqlPool)

				LogDebug("🔒 [WORKER-%d]Setting job status to 'optimizing'for %s", workerID, dbName)
				mu.Lock()
				if backupResult.Success {
					totalSizeKB += backupResult.SizeKB
					totalFull++

					// Add database disk size to total
					if dbSizeBytes, err := getDatabaseSize(dbName, mysqlPool); err == nil {
						totalDiskSizeKB += int(dbSizeBytes / 1024)
						LogDebug("📏 [DISK-SIZE] Added %d KB disk size for %s to total", int(dbSizeBytes/1024), dbName)
					} else {
						LogWarn("⚠️ [DISK-SIZE] Failed to get disk size for %s: %v", dbName, err)
					}

					LogDebug("✅ [BACKUP-SUCCESS] Worker-%d: Backup completed for %s - Size: %d KB, File: %s",
						workerID, dbName, backupResult.SizeKB, backupResult.FilePath)
				} else {
					totalFailed++
					LogError("❌ [BACKUP-FAILED] Worker-%d: Backup failed for %s: %s",
						workerID, dbName, backupResult.ErrorMessage)
				}

				// For retries, preserve existing incremental count
				incrementalCount := 0
				if request.RequestedBy == "retry" {
					incrementalCount = existingIncremental
				}
				LogDebug("📊 [SUMMARY] Updating backup summary - JobID: %s, Size: %d KB, Disk Size: %d KB, Full: %d, Incremental: %d, Failed: %d",
					request.JobID, totalSizeKB, totalDiskSizeKB, totalFull, incrementalCount, totalFailed)
				UpdateBackupSummary(request.JobID, totalSizeKB, totalDiskSizeKB, totalFull, incrementalCount, totalFailed)
				mu.Unlock()

				<-activeProcesses
				LogDebug("✅ [WORKER-%d] Process slot released, ready for next database", workerID)

				mu.Lock()
				currentActiveProcesses = config.Backup.Parallel - len(activeProcesses)
				mu.Unlock()
				LogDebug("📊 [PARALLEL-STATUS] Worker-%d completed %s, %d/%d processes currently active",
					workerID, dbName, currentActiveProcesses, config.Backup.Parallel)
			}
		}(i)
	}

	// Wait for all worker goroutines to complete
	LogInfo("⏳ [WORKERS] Waiting for all %d worker goroutines to complete", config.Backup.Parallel)
	wg.Wait()

	LogDebug("⏳ [SUMMARY] Waiting 2 seconds for job status updates to complete...")
	time.Sleep(2 * time.Second)

	LogDebug("📊 [SUMMARY] Completing backup summary for JobID: %s", request.JobID)

	// Update MySQL restart time in summary
	UpdateMySQLRestartTime(request.JobID, int(totalMySQLRestartTime))

	CompleteBackupSummary(request.JobID, config)

	LogInfo("🎉 [BACKUP-COMPLETE] Full backup job %s completed - Total: %d, Successful: %d, Failed: %d, Total Size: %d KB, Total Disk Size: %d KB",
		request.JobID, len(request.Databases), totalFull, totalFailed, totalSizeKB, totalDiskSizeKB)

	LogInfo("📊 [PARALLEL-SUMMARY] Backup job %s used %d parallel workers, processed %d databases with %d%% success rate",
		request.JobID, config.Backup.Parallel, len(request.Databases),
		int(float64(totalFull)/float64(len(request.Databases))*100))
}

// DatabaseBackupResult represents the result of a single database backup
type DatabaseBackupResult struct {
	Success      bool
	SizeKB       int
	FilePath     string
	ErrorMessage string
}

type DatabaseOptimizeResult struct {
	Success      bool
	ErrorMessage string
}

func executeDatabaseBackup(dbName, jobID string, config *Config, mysqlPool *sql.DB) DatabaseBackupResult {
	LogDebug("🏗️ [BACKUP-SETUP] Setting up backup for database: %s, JobID: %s", dbName, jobID)

	// Test MySQL connection using the shared pool (no need for timeout loop since pool is already established)
	pingCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := mysqlPool.PingContext(pingCtx)
	cancel()

	if err != nil {
		LogError("❌ [CONNECTION-TEST] MySQL connection pool ping failed for %s: %v", dbName, err)
		updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", fmt.Sprintf("MySQL connection pool ping failed: %v", err))
		if updateErr != nil {
			LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
		}
		return DatabaseBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("MySQL connection pool ping failed: %v", err),
		}
	}
	LogDebug("✅ [CONNECTION-TEST] MySQL connection pool is healthy for %s", dbName)

	backupDir := filepath.Join(config.Backup.BackupDir, dbName)
	err = os.MkdirAll(backupDir, 0755)
	if err != nil {
		LogError("❌ [DIRECTORY-ERROR] Failed to create backup directory %s: %v", backupDir, err)

		// Update job status to failed
		updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", fmt.Sprintf("Failed to create backup directory: %v", err))
		if updateErr != nil {
			LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
		}

		return DatabaseBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to create backup directory: %v", err),
		}
	}

	extension := ".sql"
	isWindows := runtime.GOOS == "windows"
	if !isWindows && config.Backup.CompressionLevel > 0 {
		extension = ".gz"
		LogDebug("🗜️ [COMPRESSION] Compression enabled (level %d), using .gz extension", config.Backup.CompressionLevel)
	} else {
		LogDebug("📄 [COMPRESSION] No compression, using .sql extension")
	}

	tempFileName := fmt.Sprintf("temp_%s_%s%s", dbName, time.Now().Format("20060102_150405.000000"), extension)
	tempFilePath := filepath.Join(backupDir, tempFileName)
	cmd := buildMysqldumpCommand(dbName, tempFilePath, config)

	startTime := time.Now()
	LogDebug("🚀 [EXECUTE] Starting mysqldump process for %s", dbName)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		LogError("❌ [EXECUTE-ERROR] Failed to get stdout pipe for %s: %v", dbName, err)
		return DatabaseBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to get stdout pipe: %v", err),
		}
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		LogError("❌ [EXECUTE-ERROR] Failed to start backup command for %s: %v", dbName, err)

		// Update job status to failed
		updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", fmt.Sprintf("Failed to start backup command: %v", err))
		if updateErr != nil {
			LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
		}

		return DatabaseBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to start backup command: %v", err),
		}
	}
	LogDebug("✅ [EXECUTE] Mysqldump process started successfully (PID: %d)", cmd.Process.Pid)

	// Get table count for progress tracking
	LogDebug("📏 [SIZE-INFO] Getting table count for %s", dbName)
	totalTables, err := getTableCount(dbName, mysqlPool)
	if err != nil {
		LogWarn("⚠️ [SIZE-INFO] Failed to get table count for %s: %v", dbName, err)
		totalTables = 1 // Fallback to prevent division by zero
	}

	// Start monitoring progress and writing to file
	go func(totalTables int) {
		// Create output file
		outputFile, err := os.Create(tempFilePath)
		if err != nil {
			LogError("❌ [EXECUTE-ERROR] Failed to create output file %s: %v", tempFilePath, err)
			// Update job status to failed
			updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", fmt.Sprintf("Failed to create output file: %v", err))
			if updateErr != nil {
				LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
			}
			return
		}
		defer outputFile.Close()

		// Process output line by line while writing to file
		LogDebug("📊 [BACKUP-MONITOR] Starting real-time progress monitoring for %s (%d tables)", dbName, totalTables)

		scanner := bufio.NewScanner(stdout)
		// Increase buffer size to handle very long lines (up to 64MB)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 64*1024*1024)

		processedTables := 0
		lastProgress := 0
		processedTableNames := make(map[string]bool) // Track processed table names to avoid duplicates

		for scanner.Scan() {
			line := scanner.Text()

			// Write line to file
			_, writeErr := outputFile.WriteString(line + "\n")
			if writeErr != nil {
				LogError("❌ [BACKUP-ERROR] Failed to write to backup file for %s: %v", dbName, writeErr)
				updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", fmt.Sprintf("File write error: %v", writeErr))
				if updateErr != nil {
					LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
				}
				// Don't close stdout here - let the main function handle it
				return
			}

			// Parse mysqldump verbose output to detect table processing
			tableDetected := false

			// Primary method: Detect table structure retrieval and extract table name
			if strings.Contains(line, "-- Retrieving table structure for table") {
				// Extract table name from the line
				parts := strings.Split(line, "table ")
				if len(parts) > 1 {
					tablePart := parts[1]
					// Remove trailing dots and spaces
					tableName := strings.TrimSuffix(tablePart, "...")
					tableName = strings.TrimSpace(tableName)
					if tableName != "" && !processedTableNames[tableName] {
						processedTables++
						tableDetected = true
						processedTableNames[tableName] = true
					}
				}
			}

			// Fallback method 1: If no table detected, count any "Retrieving table structure" line
			if !tableDetected && strings.Contains(line, "Retrieving table structure for table") {
				// Try to extract table name to avoid duplicates
				parts := strings.Split(line, "table ")
				tableName := ""
				if len(parts) > 1 {
					tablePart := parts[1]
					tableName = strings.TrimSuffix(tablePart, "...")
					tableName = strings.TrimSpace(tableName)
				}

				// Use table name if available, otherwise use a generic identifier
				identifier := tableName
				if identifier == "" {
					identifier = fmt.Sprintf("table_%d", processedTables+1)
				}

				if !processedTableNames[identifier] {
					processedTables++
					tableDetected = true
					processedTableNames[identifier] = true
				}
			}

			// Fallback method 2: If still no table detected, count INSERT INTO statements
			if !tableDetected && strings.Contains(line, "INSERT INTO `") {
				// Try to extract table name from INSERT statement for better logging
				tableName := ""
				if strings.Contains(line, "INSERT INTO `") {
					parts := strings.Split(line, "INSERT INTO `")
					if len(parts) > 1 {
						endParts := strings.Split(parts[1], "`")
						if len(endParts) > 0 {
							tableName = endParts[0]
						}
					}
				}

				// Only count if we haven't seen this table before
				if tableName != "" && !processedTableNames[tableName] {
					processedTables++
					tableDetected = true
					processedTableNames[tableName] = true
				}
			}

			// Update progress if a table was detected
			if tableDetected {
				// Calculate progress percentage
				progress := int((float64(processedTables) / float64(totalTables)) * 100)
				if progress > 100 {
					progress = 100
				}

				// Update progress if it changed significantly (every 2% or when complete)
				if progress != lastProgress && (progress%2 == 0 || progress == 100) {
					// LogInfo("📊 [BACKUP-PROGRESS] %s backup progress: %d%% (%d/%d tables)", dbName, progress, processedTables, totalTables)
					UpdateBackupJobProgress(jobID, dbName, progress)
					lastProgress = progress
				}
			}
		}

		// Check for scanner errors
		if err := scanner.Err(); err != nil {
			var errorMessage string
			if strings.Contains(err.Error(), "token too long") {
				errorMessage = "Scanner error: Line too long (max 64MB). This usually happens with large INSERT statements or binary data. Consider using --single-transaction=false or reducing --max_allowed_packet"
				LogError("❌ [BACKUP-MONITOR] Scanner error for %s: %s", dbName, errorMessage)
			} else if strings.Contains(err.Error(), "file already closed") {
				// Handle the specific "file already closed" error gracefully
				LogWarn("⚠️ [BACKUP-MONITOR] Scanner detected closed pipe for %s (this is normal when process completes)", dbName)
				LogDebug("✅ [BACKUP-MONITOR] Backup monitoring completed for %s: %d/%d tables processed (pipe closed)",
					dbName, processedTables, totalTables)
				return
			} else {
				errorMessage = fmt.Sprintf("Scanner error: %v", err)
				LogError("❌ [BACKUP-MONITOR] Scanner error for %s: %v", dbName, err)
			}

			// Only update job status to failed if it's not a "file already closed" error
			if !strings.Contains(err.Error(), "file already closed") {
				// Update job status to failed and stop the backup process
				LogError("❌ [BACKUP-ERROR] Backup failed for %s due to scanner error, updating job status to failed", dbName)
				updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", errorMessage)
				if updateErr != nil {
					LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
				}
			}
			// Don't close stdout here - let the main function handle it
			return
		}

		LogDebug("✅ [BACKUP-MONITOR] Backup monitoring completed for %s: %d/%d tables processed",
			dbName, processedTables, totalTables)
	}(totalTables)

	// Generate final filename with exact backup start time
	timestamp := startTime.Format("20060102_150405.000000")
	backupFileName := fmt.Sprintf("full_%s_%s%s", dbName, timestamp, extension)
	backupFilePath := filepath.Join(backupDir, backupFileName)
	LogDebug("📝 [FILENAME] Generated final backup filename: %s", backupFileName)
	LogDebug("📁 [FILEPATH] Final backup file path: %s", backupFilePath)

	// Reset progress to 0% when backup starts (optimization phase is complete)
	LogDebug("📊 [PROGRESS] Resetting progress to 0%% for backup phase of %s", dbName)
	UpdateBackupJobProgress(jobID, dbName, 0)

	// Progress monitoring is handled inline in the goroutine above

	// Wait for command to complete
	LogDebug("⏳ [EXECUTE] Waiting for mysqldump process to complete for %s", dbName)
	err = cmd.Wait()
	duration := time.Since(startTime)

	// Close the stdout pipe
	stdout.Close()

	if err != nil {
		var errorMessage string
		if exitError, ok := err.(*exec.ExitError); ok {
			switch exitError.ExitCode() {
			case 1:
				errorMessage = "mysqldump: General error (check database connection and permissions)"
			case 2:
				errorMessage = "mysqldump: Misuse of shell builtins"
			case 3:
				errorMessage = "mysqldump: Connection error (check host, port, username, password)"
			default:
				errorMessage = fmt.Sprintf("mysqldump failed with exit code %d: %v", exitError.ExitCode(), err)
			}
		} else {
			errorMessage = err.Error()
		}

		LogError("❌ [EXECUTE-ERROR] Backup command failed for %s after %v: %s", dbName, duration, errorMessage)

		// Clean up temporary file if backup failed
		if tempFilePath != "" {
			if removeErr := os.Remove(tempFilePath); removeErr != nil {
				LogWarn("⚠️ [CLEANUP] Failed to remove temporary file %s: %v", tempFilePath, removeErr)
			} else {
				LogDebug("🧹 [CLEANUP] Removed temporary file: %s", tempFilePath)
			}
		}

		// Update job as failed
		LogDebug("💾 [SQLITE] Updating job status to failed for %s", dbName)
		updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", errorMessage)
		if updateErr != nil {
			LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
		}

		return DatabaseBackupResult{
			Success:      false,
			ErrorMessage: errorMessage,
		}
	}
	LogDebug("✅ [EXECUTE] Mysqldump process completed successfully for %s in %v", dbName, duration)

	// Rename temporary file to final filename with retry logic
	LogDebug("📁 [RENAME] Renaming temporary file to final filename")

	// Small delay to ensure file handles are released (especially on Windows)
	time.Sleep(100 * time.Millisecond)

	finalFilePath := renameWithRetry(tempFilePath, backupFilePath, dbName)

	// Check if rename was successful
	renameSuccessful := (finalFilePath == backupFilePath)
	var errorMessage string
	if !renameSuccessful {
		errorMessage = fmt.Sprintf("Backup completed but file rename failed. Using temporary file: %s", tempFilePath)
		LogWarn("⚠️ [RENAME-WARNING] %s", errorMessage)
	}

	fileInfo, err := os.Stat(finalFilePath)
	var sizeKB int
	if err == nil {
		sizeKB = int(fileInfo.Size() / 1024)
	} else {
		LogWarn("⚠️ [SIZE] Failed to get file size for %s: %v", finalFilePath, err)
		if errorMessage == "" {
			errorMessage = fmt.Sprintf("Backup completed but failed to get file size: %v", err)
		}
	}

	// Check for any existing optimization errors
	existingError := getExistingJobError(jobID, dbName)
	if existingError != "" {
		if errorMessage != "" {
			errorMessage = fmt.Sprintf("%s; %s", existingError, errorMessage)
		} else {
			errorMessage = existingError
		}
		LogDebug("📊 [ERROR-COMBINE] Combined optimization and backup errors for %s: %s", dbName, errorMessage)
	}

	// Determine final status based on rename success and file access
	backupSuccess := renameSuccessful && err == nil
	if !backupSuccess && errorMessage == "" {
		errorMessage = "Backup completed with unknown issues"
	}

	// Update job status based on actual result
	LogDebug("💾 [SQLITE] Updating job status for %s - Success: %v, Error: %s", dbName, backupSuccess, errorMessage)
	err = CompleteBackupJob(jobID, dbName, backupSuccess, sizeKB, finalFilePath, errorMessage)
	if err != nil {
		LogError("❌ [SQLITE-ERROR] Failed to update job status for %s: %v", dbName, err)
		return DatabaseBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Backup completed but failed to update database: %v", err),
		}
	}

	if backupSuccess {
		LogDebug("🎉 [BACKUP-SUCCESS] Database backup completed successfully - DB: %s, Size: %d KB, Duration: %v, File: %s",
			dbName, sizeKB, duration, finalFilePath)
	} else {
		LogWarn("⚠️ [BACKUP-PARTIAL] Database backup completed with issues - DB: %s, Size: %d KB, Duration: %v, File: %s, Error: %s",
			dbName, sizeKB, duration, finalFilePath, errorMessage)
	}

	return DatabaseBackupResult{
		Success:      backupSuccess,
		SizeKB:       sizeKB,
		FilePath:     finalFilePath,
		ErrorMessage: errorMessage,
	}
}

// buildMysqldumpCommand builds the mysqldump command with all necessary arguments
func buildMysqldumpCommand(dbName, outputPath string, config *Config) *exec.Cmd {
	// Check if we're on Windows
	isWindows := runtime.GOOS == "windows"

	// Build the command based on platform and requirements
	var cmd *exec.Cmd

	if isWindows {
		// Windows: Simple mysqldump command (no nice, no gzip)
		cmd = buildWindowsCommand(dbName, outputPath, config)
	} else {
		// Linux: Full command with nice and optional gzip
		cmd = buildLinuxCommand(dbName, outputPath, config)
	}

	return cmd
}

// buildWindowsCommand builds command for Windows (no nice, no gzip)
func buildWindowsCommand(dbName, outputPath string, config *Config) *exec.Cmd {
	// Start with the binary path
	cmd := exec.Command(config.Database.BinaryDump)

	// Add connection arguments
	connArgs := buildMySQLConnectionArgs(config)
	cmd.Args = append(cmd.Args, connArgs...)

	// Add memory limit per process if configured
	if config.Backup.MaxMemoryPerProcess != "" {
		// Convert memory string to mysqldump format
		memoryLimit := config.Backup.MaxMemoryPerProcess
		// Ensure it's in the right format (e.g., 256M -> 256M)
		cmd.Args = append(cmd.Args, "--max_allowed_packet="+memoryLimit)
		LogDebug("Added memory limit per process: %s", memoryLimit)
	}

	// Add mysqldump options from config
	if config.Backup.MysqldumpOptions != "" {
		options := strings.Fields(config.Backup.MysqldumpOptions)
		cmd.Args = append(cmd.Args, options...)
	}

	// Add --verbose flag for progress tracking if not already present
	hasVerbose := false
	for _, arg := range cmd.Args {
		if arg == "--verbose" {
			hasVerbose = true
			break
		}
	}
	if !hasVerbose {
		cmd.Args = append(cmd.Args, "--verbose")
		LogDebug("Added --verbose flag for progress tracking")
	}

	// Add database name
	cmd.Args = append(cmd.Args, dbName)

	// Set output file - let the command handle file creation
	// This avoids file handle locking issues on Windows
	cmd.Stdout = nil // Will be set to the file when command starts

	LogDebug("Windows command built for %s: %s", dbName, strings.Join(cmd.Args, " "))
	return cmd
}

// buildLinuxCommand builds command for Linux with nice and optional gzip
func buildLinuxCommand(dbName, outputPath string, config *Config) *exec.Cmd {
	var cmd *exec.Cmd

	if config.Backup.CompressionLevel > 0 {
		// Use shell command with nice and gzip
		cmd = buildLinuxCompressedCommand(dbName, outputPath, config)
	} else {
		// Use shell command with just nice
		cmd = buildLinuxUncompressedCommand(dbName, outputPath, config)
	}

	return cmd
}

// buildLinuxCompressedCommand builds Linux command with nice and gzip
func buildLinuxCompressedCommand(dbName, outputPath string, config *Config) *exec.Cmd {
	// Build mysqldump command
	mysqldumpCmd := buildMysqldumpArgs(dbName, config)

	// Create shell command: nice -n $level mysqldump ... | gzip -$level > output.gz
	niceLevel := config.Backup.NiceLevel
	compressionLevel := config.Backup.CompressionLevel

	shellCmd := fmt.Sprintf("nice -n %d %s | gzip -%d > %s",
		niceLevel,
		strings.Join(mysqldumpCmd, " "),
		compressionLevel,
		outputPath)

	cmd := exec.Command("sh", "-c", shellCmd)

	LogInfo("Linux compressed command built for %s: %s", dbName, shellCmd)
	return cmd
}

// buildLinuxUncompressedCommand builds Linux command with just nice
func buildLinuxUncompressedCommand(dbName, outputPath string, config *Config) *exec.Cmd {
	// Build mysqldump command
	mysqldumpCmd := buildMysqldumpArgs(dbName, config)

	// Create shell command: nice -n $level mysqldump ... > output.sql
	niceLevel := config.Backup.NiceLevel

	shellCmd := fmt.Sprintf("nice -n %d %s > %s",
		niceLevel,
		strings.Join(mysqldumpCmd, " "),
		outputPath)

	cmd := exec.Command("sh", "-c", shellCmd)

	LogInfo("Linux uncompressed command built for %s: %s", dbName, shellCmd)
	return cmd
}

// buildMysqldumpArgs builds the mysqldump arguments without executing
func buildMysqldumpArgs(dbName string, config *Config) []string {
	var args []string

	// Add binary path
	args = append(args, config.Database.BinaryDump)

	// Add connection arguments
	connArgs := buildMySQLConnectionArgs(config)
	args = append(args, connArgs...)

	// Add memory limit per process if configured
	if config.Backup.MaxMemoryPerProcess != "" {
		// Convert memory string to mysqldump format
		memoryLimit := config.Backup.MaxMemoryPerProcess
		// Ensure it's in the right format (e.g., 256M -> 256M)
		args = append(args, "--max_allowed_packet="+memoryLimit)
		LogDebug("Added memory limit per process: %s", memoryLimit)
	}

	// Add mysqldump options from config
	if config.Backup.MysqldumpOptions != "" {
		options := strings.Fields(config.Backup.MysqldumpOptions)
		args = append(args, options...)
	}

	// Add --verbose flag for progress tracking if not already present
	hasVerbose := false
	for _, arg := range args {
		if arg == "--verbose" {
			hasVerbose = true
			break
		}
	}
	if !hasVerbose {
		args = append(args, "--verbose")
	}

	// Add database name
	args = append(args, dbName)

	return args
}

// GetEstimatedRowCount gets estimated row count for a database
func GetEstimatedRowCount(dbName string, config *Config) (int, error) {
	// Build connection string
	dsn, err := buildMySQLDSN(config)
	if err != nil {
		return 0, err
	}

	// Connect to MySQL
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		return 0, err
	}

	// Query for estimated row count
	query := `
		SELECT SUM(table_rows) 
		FROM information_schema.tables 
		WHERE table_schema = ? 
		AND table_type = 'BASE TABLE'
	`

	var totalRows sql.NullInt64
	err = db.QueryRow(query, dbName).Scan(&totalRows)
	if err != nil {
		return 0, err
	}

	if totalRows.Valid {
		return int(totalRows.Int64), nil
	}

	return 0, nil
}

// MonitorBackupProgress monitors the progress of a backup by parsing mysqldump output
func MonitorBackupProgress(cmd *exec.Cmd, dbName string, estimatedRows int, jobID string) {
	// Get the process
	process := cmd.Process
	if process == nil {
		LogWarn("Cannot monitor progress for %s - process not available", dbName)
		return
	}

	LogDebug("Starting process monitoring for %s (PID: %d)", dbName, process.Pid)

	// Create a channel to signal when process is done
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Monitor process until it completes
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	progress := 0
	for {
		select {
		case err := <-done:
			// Process has finished
			if err != nil {
				LogWarn("Process monitoring completed for %s (PID: %d) with error: %v", dbName, process.Pid, err)
			}
			return

		case <-ticker.C:
			LogDebug("Process %s (PID: %d) is still running", dbName, process.Pid)
			progress += 10
			if progress > 90 {
				progress = 90
			}
			UpdateBackupJobProgress(jobID, dbName, progress)
		}
	}
}

// MonitorBackupProgressWithSize monitors backup progress based on file size growth
func MonitorBackupProgressWithSize(cmd *exec.Cmd, dbName, jobID string, dbSizeBytes int64, tableSizes []TableSize, backupFilePath string) {
	LogDebug("📊 [BACKUP-MONITOR] Starting real progress monitoring for %s (DB Size: %d bytes)", dbName, dbSizeBytes)

	// Get the process
	process := cmd.Process
	if process == nil {
		LogWarn("⚠️ [BACKUP-MONITOR] Cannot monitor progress for %s - process not available", dbName)
		return
	}

	LogDebug("📊 [BACKUP-MONITOR] Starting real progress monitoring for %s (PID: %d, DB Size: %.2f MB)",
		dbName, process.Pid, float64(dbSizeBytes)/(1024*1024))

	// Create a channel to receive process completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Monitor process until it completes
	ticker := time.NewTicker(2 * time.Second) // Check every 2 seconds for more responsive updates
	defer ticker.Stop()

	lastFileSize := int64(0)
	lastProgress := 0
	startTime := time.Now()

	for {
		select {
		case err := <-done:
			// Process has finished
			if err != nil {
				var errorMessage string
				if exitError, ok := err.(*exec.ExitError); ok {
					switch exitError.ExitCode() {
					case 1:
						errorMessage = "mysqldump: General error (check database connection and permissions)"
					case 2:
						errorMessage = "mysqldump: Misuse of shell builtins"
					case 3:
						errorMessage = "mysqldump: Connection error (check host, port, username, password)"
					default:
						errorMessage = fmt.Sprintf("mysqldump failed with exit code %d: %v", exitError.ExitCode(), err)
					}
				} else {
					errorMessage = fmt.Sprintf("Backup process failed: %v", err)
				}
				LogWarn("⚠️ [BACKUP-MONITOR] Backup process %s completed with error: %s", dbName, errorMessage)
				// Update job status to failed with error message
				updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", errorMessage)
				if updateErr != nil {
					LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
				}
			} else {
				LogDebug("✅ [BACKUP-MONITOR] Backup process %s completed successfully", dbName)
				// Set final progress to 100%
				UpdateBackupJobProgress(jobID, dbName, 100)
				// Note: Don't update status to "done" here - let the main backup function handle completion
				// with proper file path and size information
			}
			return

		case <-ticker.C:
			// Check current file size
			fileInfo, err := os.Stat(backupFilePath)
			if err != nil {
				LogDebug("📊 [BACKUP-MONITOR] Cannot get file size for %s: %v", dbName, err)
				continue
			}

			currentFileSize := fileInfo.Size()

			// Calculate progress based on file size growth
			var progress int
			if dbSizeBytes > 0 {
				// Estimate progress based on file size vs database size
				// Note: Backup file is typically 2-3x larger than original DB due to SQL format
				estimatedBackupSize := dbSizeBytes * 3 // Conservative estimate
				progress = int((float64(currentFileSize) / float64(estimatedBackupSize)) * 100)

				// Cap progress at 95% until process completes
				if progress > 95 {
					progress = 95
				}
			} else {
				// Fallback: time-based progress if we can't get DB size
				elapsed := time.Since(startTime)
				progress = int(elapsed.Minutes() * 2) // Assume 2% per minute
				if progress > 90 {
					progress = 90
				}
			}

			// Only update if progress changed significantly or file size increased
			if progress != lastProgress && (progress%5 == 0 || currentFileSize > lastFileSize) {
				// sizeKB := currentFileSize / 1024
				UpdateBackupJobProgress(jobID, dbName, progress)
				lastProgress = progress
			}

			lastFileSize = currentFileSize
		}
	}
}

// getCurrentMemoryUsage returns current system memory usage percentage
func getCurrentMemoryUsage() float64 {
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		LogError("❌ [MEMORY-ERROR] Failed to get memory usage: %v", err)
		return 0.0
	}

	// Log detailed memory information for debugging
	LogDebug("🧠 [MEMORY-DETAILS] Total: %.2f GB, Used: %.2f GB, Available: %.2f GB, Usage: %.2f%%",
		float64(vmStat.Total)/(1024*1024*1024),
		float64(vmStat.Used)/(1024*1024*1024),
		float64(vmStat.Available)/(1024*1024*1024),
		vmStat.UsedPercent)

	return vmStat.UsedPercent
}

func shouldRestartMySQL(config *Config) bool {
	if runtime.GOOS != "linux" {
		return false
	}

	memoryPercent := getCurrentMemoryUsage()
	threshold := float64(config.Backup.MaxMemoryThreshold)

	if memoryPercent > threshold {
		LogWarn("⚠️ [MEMORY-THRESHOLD] Memory usage %.2f%% exceeds threshold %.2f%% - MySQL restart required",
			memoryPercent, threshold)
		return true
	}

	return false
}

// restartMySQLServiceWithMonitoring restarts MySQL service and monitors it for 10 minutes
// Returns (success bool, duration time.Duration) for the restart operation
func restartMySQLServiceWithMonitoring(config *Config, abortBackup *bool) (bool, time.Duration) {
	restartStartTime := time.Now()
	if runtime.GOOS != "linux" {
		LogInfo("⏭️ [MYSQL-RESTART] MySQL restart skipped - not on Linux system (OS: %s)", runtime.GOOS)
		return true, 0
	}

	// Get memory usage before restart
	LogDebug("🧠 [MEMORY-BEFORE] Getting memory usage before MySQL restart")
	vmStatBefore, err := mem.VirtualMemory()
	memoryBefore := 0.0
	if err == nil {
		memoryBefore = vmStatBefore.UsedPercent
		LogDebug("🧠 [MEMORY-BEFORE] Memory usage before restart: %.2f%%", memoryBefore)
	} else {
		LogWarn("⚠️ [MEMORY-BEFORE] Failed to get memory usage before restart: %v", err)
	}

	LogWarn("🔄 [MYSQL-RESTART] Restarting MySQL service due to high memory usage (%.2f%% > %d%%)...",
		memoryBefore, config.Backup.MaxMemoryThreshold)

	// Try different MySQL service names
	serviceNames := []string{"mysql", "mariadb", "mysqld"}
	var restartedService string

	for _, serviceName := range serviceNames {
		LogDebug("🔄 [MYSQL-RESTART] Attempting to restart service '%s' via systemctl", serviceName)
		cmd := exec.Command("sudo", "systemctl", "restart", serviceName)
		err := cmd.Run()
		if err == nil {
			restartedService = serviceName
			LogInfo("✅ [MYSQL-RESTART] MySQL service '%s' restart command executed successfully", serviceName)
			break
		} else {
			LogDebug("❌ [MYSQL-RESTART] Failed to restart service '%s' via systemctl: %v", serviceName, err)
		}
	}

	// If systemctl fails, try service command
	if restartedService == "" {
		LogWarn("⚠️ [MYSQL-RESTART] systemctl failed, trying service command...")
		for _, serviceName := range serviceNames {
			LogDebug("🔄 [MYSQL-RESTART] Attempting to restart service '%s' via service command", serviceName)
			cmd := exec.Command("sudo", "service", serviceName, "restart")
			err := cmd.Run()
			if err == nil {
				restartedService = serviceName
				LogInfo("✅ [MYSQL-RESTART] MySQL service '%s' restart command executed successfully (via service command)", serviceName)
				break
			} else {
				LogDebug("❌ [MYSQL-RESTART] Failed to restart service '%s' via service command: %v", serviceName, err)
			}
		}
	}

	if restartedService == "" {
		LogError("❌ [MYSQL-RESTART] Failed to restart MySQL service - tried all common service names (mysql, mariadb, mysqld)")
		return false, time.Since(restartStartTime)
	}

	// Monitor service for up to 10 minutes
	LogInfo("⏳ [MYSQL-MONITOR] Monitoring MySQL service '%s' for up to 10 minutes...", restartedService)

	monitorDuration := 10 * time.Minute
	checkInterval := 30 * time.Second
	startTime := time.Now()
	checkCount := 0

	for time.Since(startTime) < monitorDuration {
		time.Sleep(checkInterval)
		checkCount++

		// Check if service is running
		if isMySQLServiceRunning(restartedService) {
			// Get memory usage after restart
			vmStatAfter, err := mem.VirtualMemory()
			memoryAfter := 0.0
			if err == nil {
				memoryAfter = vmStatAfter.UsedPercent
			}

			LogInfo("✅ [MYSQL-MONITOR] MySQL service '%s' is running successfully! Memory: %.2f%% (before: %.2f%%)",
				restartedService, memoryAfter, memoryBefore)
			restartDuration := time.Since(restartStartTime)
			LogInfo("⏱️ [MYSQL-RESTART-TIME] MySQL restart took %v", restartDuration)
			return true, restartDuration
		}

		elapsed := time.Since(startTime)
		remaining := monitorDuration - elapsed
		LogWarn("⏳ [MYSQL-MONITOR] MySQL service '%s' not yet running, waiting... (Check #%d, %.1f minutes remaining)",
			restartedService, checkCount, remaining.Minutes())
	}

	// Service failed to start within 10 minutes
	LogError("❌ [MYSQL-MONITOR] MySQL service '%s' failed to start within 10 minutes, aborting all backup processes", restartedService)
	*abortBackup = true
	return false, time.Since(restartStartTime)
}

// isMySQLServiceRunning checks if MySQL service is running
func isMySQLServiceRunning(serviceName string) bool {
	// Try systemctl status first
	LogDebug("🔍 [MYSQL-CHECK] Checking if service '%s' is running via systemctl", serviceName)
	cmd := exec.Command("sudo", "systemctl", "is-active", serviceName)
	output, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "active" {
		LogDebug("✅ [MYSQL-CHECK] Service '%s' is active via systemctl", serviceName)
		return true
	}

	// Try service status as fallback
	LogDebug("🔍 [MYSQL-CHECK] systemctl failed, trying service command for '%s'", serviceName)
	cmd = exec.Command("sudo", "service", serviceName, "status")
	err = cmd.Run()
	if err == nil {
		LogDebug("✅ [MYSQL-CHECK] Service '%s' is running via service command", serviceName)
		return true
	}

	LogDebug("❌ [MYSQL-CHECK] Service '%s' is not running", serviceName)
	return false
}

// optimizeDatabaseWithProgress runs mariadb-check/mysqlcheck with real progress tracking
func optimizeDatabaseWithProgress(dbName, jobID string, config *Config, mysqlPool *sql.DB) DatabaseOptimizeResult {
	LogDebug("🔧 [OPTIMIZE-CONFIG] Optimization settings - Binary: %s, Options: %s",
		config.Database.BinaryCheck, config.Backup.MariadbCheckOptions)

	// First, get total table count for progress calculation
	totalTables, err := getTableCount(dbName, mysqlPool)
	if err != nil {
		LogWarn("⚠️ [OPTIMIZE-TABLES] Failed to get table count for %s: %v", dbName, err)
		totalTables = 1 // Fallback to prevent division by zero
	}

	cmd := buildOptimizeCommand(dbName, config)
	if cmd == nil {
		LogError("❌ [OPTIMIZE-ERROR] Failed to build optimization command for %s", dbName)
		return DatabaseOptimizeResult{
			Success:      false,
			ErrorMessage: "Failed to build optimization command",
		}
	}

	startTime := time.Now()

	// FIXED: Get stdout pipe before starting the command (mariadb-check outputs to stdout)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		LogError("❌ [OPTIMIZE-ERROR] Failed to get stdout pipe for %s: %v", dbName, err)
		return DatabaseOptimizeResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to get stdout pipe: %v", err),
		}
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		LogError("❌ [OPTIMIZE-ERROR] Failed to start optimization command for %s: %v", dbName, err)
		return DatabaseOptimizeResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to start optimization command: %v", err),
		}
	}

	// Start monitoring progress (command is already started, pass stdout pipe)
	go MonitorOptimizationProgressWithPipe(stdout, dbName, jobID, totalTables)

	// Wait for the command to complete
	err = cmd.Wait()
	duration := time.Since(startTime)

	if err != nil {
		LogError("❌ [OPTIMIZE-ERROR] Database optimization failed for %s after %v: %v", dbName, duration, err)
		return DatabaseOptimizeResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	LogDebug("✅ [OPTIMIZE-SUCCESS] Database optimization completed for %s in %v", dbName, duration)
	return DatabaseOptimizeResult{
		Success: true,
	}
}

// optimizeDatabase runs mariadb-check/mysqlcheck to optimize database before backup (legacy function)
func optimizeDatabase(dbName string, config *Config) DatabaseOptimizeResult {
	LogDebug("🔧 [OPTIMIZE-START] Starting database optimization for %s", dbName)
	LogDebug("🔧 [OPTIMIZE-CONFIG] Optimization settings - Binary: %s, Options: %s",
		config.Database.BinaryCheck, config.Backup.MariadbCheckOptions)

	cmd := buildOptimizeCommand(dbName, config)
	if cmd == nil {
		LogError("❌ [OPTIMIZE-ERROR] Failed to build optimization command for %s", dbName)
		return DatabaseOptimizeResult{
			Success:      false,
			ErrorMessage: "Failed to build optimization command",
		}
	}

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	if err != nil {
		LogError("❌ [OPTIMIZE-ERROR] Database optimization failed for %s after %v: %v", dbName, duration, err)
		return DatabaseOptimizeResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	LogDebug("✅ [OPTIMIZE-SUCCESS] Database optimization completed for %s in %v", dbName, duration)
	return DatabaseOptimizeResult{
		Success: true,
	}
}

// buildOptimizeCommand builds the mariadb-check/mysqlcheck command for database optimization
func buildOptimizeCommand(dbName string, config *Config) *exec.Cmd {
	// Check if we're on Windows
	isWindows := runtime.GOOS == "windows"

	// Build the command based on platform
	var cmd *exec.Cmd

	if isWindows {
		// Windows: Simple mariadb-check command
		cmd = buildWindowsOptimizeCommand(dbName, config)
	} else {
		// Linux: Command with nice priority
		cmd = buildLinuxOptimizeCommand(dbName, config)
	}

	return cmd
}

// buildWindowsOptimizeCommand builds optimization command for Windows
func buildWindowsOptimizeCommand(dbName string, config *Config) *exec.Cmd {
	// Start with the binary path
	cmd := exec.Command(config.Database.BinaryCheck)

	// Add connection arguments
	connArgs := buildMySQLConnectionArgs(config)
	cmd.Args = append(cmd.Args, connArgs...)

	// Add optimization options from config
	if config.Backup.MariadbCheckOptions != "" {
		options := strings.Fields(config.Backup.MariadbCheckOptions)
		cmd.Args = append(cmd.Args, options...)
	}

	// Add database name
	cmd.Args = append(cmd.Args, dbName)

	LogDebug("Windows optimization command built for %s: %s", dbName, strings.Join(cmd.Args, " "))
	return cmd
}

// buildLinuxOptimizeCommand builds optimization command for Linux with nice priority
func buildLinuxOptimizeCommand(dbName string, config *Config) *exec.Cmd {
	// Build mariadb-check command arguments
	checkCmd := buildOptimizeArgs(dbName, config)

	// Create shell command with nice priority
	niceLevel := config.Backup.NiceLevel
	shellCmd := fmt.Sprintf("nice -n %d %s", niceLevel, strings.Join(checkCmd, " "))

	cmd := exec.Command("sh", "-c", shellCmd)

	LogDebug("Linux optimization command built for %s: %s", dbName, shellCmd)
	return cmd
}

// buildOptimizeArgs builds the mariadb-check arguments without executing
func buildOptimizeArgs(dbName string, config *Config) []string {
	var args []string

	// Add binary path
	args = append(args, config.Database.BinaryCheck)

	// Add connection arguments
	connArgs := buildMySQLConnectionArgs(config)
	args = append(args, connArgs...)

	// Add optimization options from config
	if config.Backup.MariadbCheckOptions != "" {
		options := strings.Fields(config.Backup.MariadbCheckOptions)
		args = append(args, options...)
	}

	// Add database name
	args = append(args, dbName)

	return args
}

// getTableCount gets the total number of tables in a database
func getTableCount(dbName string, mysqlPool *sql.DB) (int, error) {
	// Query table count using shared connection pool
	query := "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ?"
	var count int
	err := mysqlPool.QueryRow(query, dbName).Scan(&count)
	if err != nil {
		LogError("❌ [TABLE-COUNT] Failed to query table count: %v", err)
		return 0, err
	}

	return count, nil
}

// getDatabaseSize gets the total size of a database in bytes
func getDatabaseSize(dbName string, mysqlPool *sql.DB) (int64, error) {
	// Query database size using shared connection pool
	query := `
		SELECT 
			COALESCE(SUM(data_length + index_length), 0) as size_bytes
		FROM information_schema.tables 
		WHERE table_schema = ?
	`
	var sizeBytes int64
	err := mysqlPool.QueryRow(query, dbName).Scan(&sizeBytes)
	if err != nil {
		LogError("❌ [DB-SIZE] Failed to query database size: %v", err)
		return 0, err
	}

	sizeKB := sizeBytes / 1024
	sizeMB := sizeKB / 1024
	LogDebug("📏 [DB-SIZE] Database %s size: %d bytes (%d KB, %.2f MB)", dbName, sizeBytes, sizeKB, float64(sizeMB))
	return sizeBytes, nil
}

// getTableSizes gets individual table sizes for more granular progress tracking
func getTableSizes(dbName string, mysqlPool *sql.DB) ([]TableSize, error) {
	LogDebug("📊 [TABLE-SIZES] Getting individual table sizes for: %s", dbName)

	// Query table sizes using shared connection pool
	query := `
		SELECT 
			table_name,
			COALESCE(data_length + index_length, 0) as size_bytes,
			COALESCE(table_rows, 0) as row_count
		FROM information_schema.tables 
		WHERE table_schema = ?
		ORDER BY (data_length + index_length) DESC
	`

	rows, err := mysqlPool.Query(query, dbName)
	if err != nil {
		LogError("❌ [TABLE-SIZES] Failed to query table sizes: %v", err)
		return nil, err
	}
	defer rows.Close()

	var tableSizes []TableSize
	for rows.Next() {
		var tableSize TableSize
		err := rows.Scan(&tableSize.TableName, &tableSize.SizeBytes, &tableSize.RowCount)
		if err != nil {
			LogError("❌ [TABLE-SIZES] Failed to scan table size: %v", err)
			continue
		}
		tableSizes = append(tableSizes, tableSize)
	}

	LogDebug("📊 [TABLE-SIZES] Found %d tables in database %s", len(tableSizes), dbName)
	return tableSizes, nil
}

// TableSize represents the size information of a database table
type TableSize struct {
	TableName string
	SizeBytes int64
	RowCount  int64
}

// MonitorOptimizationProgressWithPipe monitors mysqlcheck stdout output for real progress tracking
func MonitorOptimizationProgressWithPipe(stdout io.ReadCloser, dbName, jobID string, totalTables int) {
	scanner := bufio.NewScanner(stdout)
	processedTables := 0
	lastProgress := 0

	// Read output line by line
	for scanner.Scan() {
		line := scanner.Text()
		tableDetected := false

		if strings.Contains(line, dbName+".") {
			parts := strings.Split(line, dbName+".")
			if len(parts) > 1 {
				tablePart := parts[1]
				tableName := strings.Fields(tablePart)[0]
				tableName = strings.TrimRight(tableName, ":-")
				if tableName != "" {
					processedTables++
					tableDetected = true
				}
			}
		}

		if !tableDetected && strings.Contains(line, "status") && strings.Contains(line, "OK") {
			processedTables++
			tableDetected = true
		}

		if tableDetected {
			progress := int((float64(processedTables) / float64(totalTables)) * 100)
			if progress > 100 {
				progress = 100
			}

			if progress != lastProgress && (progress%2 == 0 || progress == 100) {
				UpdateBackupJobProgress(jobID, dbName, progress)
				lastProgress = progress
			}
		}
	}

	if err := scanner.Err(); err != nil {
		LogError("❌ [OPTIMIZE-MONITOR] Scanner error for %s: %v", dbName, err)
	}

	stdout.Close()

	LogDebug("✅ [OPTIMIZE-MONITOR] Optimization monitoring completed for %s: %d/%d tables processed",
		dbName, processedTables, totalTables)
}

// MonitorOptimizationProgress monitors mysqlcheck output for real progress tracking
func MonitorOptimizationProgress(cmd *exec.Cmd, dbName, jobID string, totalTables int) {
	LogDebug("📊 [OPTIMIZE-MONITOR] Starting real-time progress monitoring for %s (%d tables)", dbName, totalTables)

	// FIXED: Don't call cmd.Start() here since it's already started
	// Get the command's stdout pipe to read mysqlcheck output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		LogError("❌ [OPTIMIZE-MONITOR] Failed to get stdout pipe for %s: %v", dbName, err)
		return
	}

	// Create a scanner to read output line by line
	scanner := bufio.NewScanner(stdout)
	processedTables := 0
	lastProgress := 0

	// Read output line by line
	for scanner.Scan() {
		line := scanner.Text()

		// Parse mysqlcheck output to detect table processing
		// mysqlcheck typically outputs lines like:
		// "database.table_name" - Table is up to date
		// "database.table_name" - Table is corrupted
		// "database.table_name" - Table needs to be checked
		// "database.table_name" - status   : OK
		// "database.table_name" - note     : Table does not support optimize

		tableDetected := false

		// Primary method: Check if this line contains a table name (database.table_name pattern)
		if strings.Contains(line, dbName+".") {
			// Extract table name from the line
			// Format: "database.table_name" or "database.table_name - status"
			parts := strings.Split(line, dbName+".")
			if len(parts) > 1 {
				tablePart := parts[1]
				// Get the table name (everything before the first space, dash, or colon)
				tableName := strings.Fields(tablePart)[0]
				// Remove any trailing characters like colons, dashes, etc.
				tableName = strings.TrimRight(tableName, ":-")
				if tableName != "" {
					processedTables++
					tableDetected = true
				}
			}
		}

		if !tableDetected && strings.Contains(line, "status") && strings.Contains(line, "OK") {
			processedTables++
			tableDetected = true
		}

		if tableDetected {
			progress := int((float64(processedTables) / float64(totalTables)) * 100)
			if progress > 100 {
				progress = 100
			}

			if progress != lastProgress && (progress%2 == 0 || progress == 100) {
				UpdateBackupJobProgress(jobID, dbName, progress)
				lastProgress = progress
			}
		}
	}

	// Note: Don't call cmd.Wait() here since it's handled by the caller
	LogDebug("✅ [OPTIMIZE-MONITOR] Optimization monitoring completed for %s: %d/%d tables processed",
		dbName, processedTables, totalTables)
}

// renameWithRetry attempts to rename a file with retry logic to handle file locking issues
func renameWithRetry(tempFilePath, backupFilePath, dbName string) string {
	maxRetries := 5
	retryDelay := 100 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		LogDebug("📁 [RENAME] Attempt %d/%d to rename %s to %s", attempt, maxRetries, tempFilePath, backupFilePath)

		// Check if temp file exists
		if _, err := os.Stat(tempFilePath); os.IsNotExist(err) {
			LogWarn("⚠️ [RENAME] Temporary file %s does not exist, using backup path", tempFilePath)
			return backupFilePath
		}

		// Attempt to rename
		if err := os.Rename(tempFilePath, backupFilePath); err != nil {
			if attempt == maxRetries {
				LogError("❌ [RENAME-ERROR] Failed to rename backup file after %d attempts from %s to %s: %v",
					maxRetries, tempFilePath, backupFilePath, err)
				LogWarn("⚠️ [RENAME] Using temporary file path as fallback: %s", tempFilePath)
				return tempFilePath
			}

			LogWarn("⚠️ [RENAME] Attempt %d failed for %s: %v, retrying in %v",
				attempt, dbName, err, retryDelay)

			// Wait before retry with exponential backoff
			time.Sleep(retryDelay)
			retryDelay *= 2 // Exponential backoff

			// Force garbage collection to release any file handles
			runtime.GC()

			// On Windows, try to force release any remaining file handles
			if runtime.GOOS == "windows" {
				// Additional delay for Windows file system
				time.Sleep(50 * time.Millisecond)
			}

			continue
		}

		// Success
		LogDebug("✅ [RENAME] Successfully renamed backup file to: %s", backupFilePath)
		return backupFilePath
	}

	// This should never be reached, but just in case
	LogWarn("⚠️ [RENAME] Unexpected end of retry loop, using temporary file path: %s", tempFilePath)
	return tempFilePath
}
