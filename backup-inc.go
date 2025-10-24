package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// BackupIncRequest represents a manual incremental backup request from web UI
type BackupIncRequest struct {
	JobID       string   `json:"job_id"`
	Databases   []string `json:"databases"`
	BackupMode  string   `json:"backup_mode"`
	RequestedBy string   `json:"requested_by"`
}

// BackupIncResponse represents the response after starting incremental backup
type BackupIncResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	JobID   string `json:"job_id"`
	Details struct {
		BackupMode string   `json:"backup_mode"`
		Databases  []string `json:"databases"`
		TotalDBs   int      `json:"total_dbs"`
	} `json:"details"`
}

// StartIncBackup starts a manual incremental backup process
func StartIncBackup(request BackupIncRequest) BackupIncResponse {
	LogInfo("🚀 [INC-BACKUP-START] Starting manual incremental backup - JobID: %s, Databases: %v, Mode: %s, RequestedBy: %s",
		request.JobID, request.Databases, request.BackupMode, request.RequestedBy)

	config, err := loadConfig("config.json")
	if err != nil {
		LogError("❌ [CONFIG-ERROR] Failed to load config for incremental backup: %v", err)
		return BackupIncResponse{
			Success: false,
			Message: "Failed to load configuration",
		}
	}

	if len(request.Databases) == 0 {
		LogWarn("⚠️ [VALIDATION] No databases specified for incremental backup")
		return BackupIncResponse{
			Success: false,
			Message: "No databases specified for incremental backup",
		}
	}

	// Check binlog format - fallback to full backup if not STATEMENT
	if testState.BinlogFormat != "STATEMENT" {
		LogWarn("⚠️ [BINLOG-FORMAT] Binlog format is %s (not STATEMENT) - falling back to full backup", testState.BinlogFormat)
		return convertToFullBackup(request)
	}

	LogDebug("✅ [VALIDATION] Database list validated - Count: %d", len(request.Databases))

	err = CreateBackupSummary(request.JobID, request.BackupMode, len(request.Databases))
	if err != nil {
		LogError("❌ [SQLITE-ERROR] Failed to create backup summary: %v", err)
		return BackupIncResponse{
			Success: false,
			Message: "Failed to create backup summary in database",
		}
	}

	go executeIncBackup(request, config)

	response := BackupIncResponse{
		Success: true,
		Message: "Incremental backup process started successfully",
		JobID:   request.JobID,
	}
	response.Details.BackupMode = request.BackupMode
	response.Details.Databases = request.Databases
	response.Details.TotalDBs = len(request.Databases)

	return response
}

// convertToFullBackup converts incremental backup request to full backup
func convertToFullBackup(request BackupIncRequest) BackupIncResponse {
	LogInfo("🔄 [FALLBACK] Converting incremental backup to full backup due to binlog format")

	// Create full backup request
	fullRequest := BackupFullRequest(request)

	// Start full backup
	go StartFullBackup(fullRequest)

	return BackupIncResponse{
		Success: true,
		Message: "Incremental backup converted to full backup due to binlog format",
		JobID:   request.JobID,
	}
}

// getLatestBackupTime finds the latest backup timestamp for a database (both full and incremental)
func getLatestBackupTime(dbName string, config *Config) (time.Time, string, error) {
	backupDir := filepath.Join(config.Backup.BackupDir, dbName)

	// Check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return time.Time{}, "", fmt.Errorf("backup directory not found for %s", dbName)
	}

	var allMatches []string

	// Look for ALL backup files (both full and incremental)
	// Search for full backup files
	fullBackupPattern := fmt.Sprintf("full_%s_*.sql", dbName)
	fullMatches, err := filepath.Glob(filepath.Join(backupDir, fullBackupPattern))
	if err != nil {
		LogWarn("Error searching for full backup files for %s: %v", dbName, err)
	} else {
		allMatches = append(allMatches, fullMatches...)
	}

	// Search for full .gz files
	fullGzPattern := fmt.Sprintf("full_%s_*.gz", dbName)
	fullGzMatches, err := filepath.Glob(filepath.Join(backupDir, fullGzPattern))
	if err != nil {
		LogWarn("Error searching for compressed full backup files for %s: %v", dbName, err)
	} else {
		allMatches = append(allMatches, fullGzMatches...)
	}

	// Search for incremental backup files
	incBackupPattern := fmt.Sprintf("inc_%s_*.sql", dbName)
	incMatches, err := filepath.Glob(filepath.Join(backupDir, incBackupPattern))
	if err != nil {
		LogWarn("Error searching for incremental backup files for %s: %v", dbName, err)
	} else {
		allMatches = append(allMatches, incMatches...)
	}

	// Search for incremental .gz files
	incGzPattern := fmt.Sprintf("inc_%s_*.gz", dbName)
	incGzMatches, err := filepath.Glob(filepath.Join(backupDir, incGzPattern))
	if err != nil {
		LogWarn("Error searching for compressed incremental backup files for %s: %v", dbName, err)
	} else {
		allMatches = append(allMatches, incGzMatches...)
	}

	if len(allMatches) == 0 {
		return time.Time{}, "", fmt.Errorf("no backup files found for %s", dbName)
	}

	// Find the most recent backup (regardless of type - full or incremental)
	var latestBackup string
	var latestTime time.Time

	for _, match := range allMatches {
		fileName := filepath.Base(match)
		nameWithoutExt := strings.TrimSuffix(fileName, ".sql")
		nameWithoutExt = strings.TrimSuffix(nameWithoutExt, ".gz")

		parts := strings.Split(nameWithoutExt, "_")
		if len(parts) >= 3 {
			// For format: inc_dbname_20251001_224620.950678 or full_dbname_20251001_224620.950678
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
		return time.Time{}, "", fmt.Errorf("no valid backup timestamp found for %s", dbName)
	}

	// Add 1 microsecond to avoid duplicate queries and restore errors
	// This ensures the next incremental backup starts AFTER the last backup completed
	latestTime = latestTime.Add(1 * time.Microsecond)

	LogDebug("📅 [BACKUP-TIME] Found latest backup for %s: %s", dbName, latestBackup)
	LogDebug("📅 [BACKUP-TIME] Original timestamp: %s", latestTime.Add(-1*time.Microsecond).Format("2006-01-02 15:04:05.000000"))
	LogDebug("📅 [BACKUP-TIME] Adjusted start time (+1μs): %s", latestTime.Format("2006-01-02 15:04:05.000000"))
	LogDebug("📅 [BACKUP-TIME] This ensures no duplicate queries will be executed")

	return latestTime, latestBackup, nil
}

// executeIncBackup executes the incremental backup process
func executeIncBackup(request BackupIncRequest, config *Config) {
	totalSizeKB := 0
	totalDiskSizeKB := 0
	totalInc := 0
	totalFailed := 0

	backupType := "force-inc"
	if request.BackupMode == "auto" {
		backupType = "auto-inc"
	}

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
	LogInfo("👥 [WORKERS] Starting %d worker goroutines for parallel incremental processing", config.Backup.Parallel)
	for i := 0; i < config.Backup.Parallel; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for dbName := range processQueue {
				// Check for global abort signal first
				if CheckGlobalBackupAbort() {
					LogWarn("⚠️ [WORKER-%d] Incremental backup aborted due to global stop signal", workerID)
					return
				}

				mu.Lock()
				if abortBackup {
					mu.Unlock()
					LogWarn("⚠️ [WORKER-%d] Incremental backup aborted due to MySQL service failure", workerID)

					// Update job status to failed due to abort
					updateErr := CompleteBackupJob(request.JobID, dbName, false, 0, "", "Incremental backup aborted due to MySQL service failure")
					if updateErr != nil {
						LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
					}

					return
				}
				mu.Unlock()

				// Check memory threshold before acquiring process slot (Linux only)
				if runtime.GOOS == "linux" && shouldRestartMySQL(config) {
					LogWarn("⚠️ [MEMORY] Memory threshold exceeded (%.2f%% > %d%%), waiting for memory to be freed before starting %s",
						getCurrentMemoryUsage(), config.Backup.MaxMemoryThreshold, dbName)

					// Wait for memory to be freed or MySQL restart to complete
					for shouldRestartMySQL(config) {
						time.Sleep(5 * time.Second)
						if CheckGlobalBackupAbort() {
							LogWarn("⚠️ [WORKER-%d] Incremental backup aborted while waiting for memory threshold", workerID)
							return
						}
					}
					LogInfo("✅ [MEMORY] Memory threshold cleared, proceeding with %s", dbName)
				}

				activeProcesses <- struct{}{}

				// Check for global abort before starting backup
				if CheckGlobalBackupAbort() {
					LogWarn("⚠️ [WORKER-%d] Incremental backup aborted before starting %s", workerID, dbName)
					<-activeProcesses
					return
				}

				LogInfo("🚀 [WORKER-%d] Starting incremental backup for database: %s", workerID, dbName)

				// Get latest backup time for --start-datetime (incremental or full)
				latestBackupTime, latestBackupFile, err := getLatestBackupTime(dbName, config)
				if err != nil {
					LogWarn("⚠️ [BACKUP-TIME] Failed to get latest backup time for %s: %v - will use current time", dbName, err)
					latestBackupTime = time.Now().Add(-1 * time.Hour) // Fallback to 1 hour ago
				} else {
					LogInfo("📅 [BACKUP-TIME] Using latest backup time for %s: %s (from file: %s, adjusted +1μs to avoid duplicates)",
						dbName, latestBackupTime.Format("2006-01-02 15:04:05.000000"), latestBackupFile)
				}

				estimatedSizeKB := 0
				if dbSizeBytes, err := getDatabaseSize(dbName, config); err == nil {
					estimatedSizeKB = int(dbSizeBytes / 1024)
				} else {
					LogWarn("⚠️ [ESTIMATE] Failed to get estimated size for %s: %v", dbName, err)
				}

				err = CreateBackupJob(request.JobID, dbName, backupType, estimatedSizeKB)
				if err != nil {
					LogError("❌ [SQLITE-ERROR] Failed to create job for %s: %v", dbName, err)
				} else {
					LogDebug("✅ [SQLITE] Job record created for %s", dbName)
					go broadcastJobsUpdate()
				}

				mu.Lock()
				currentActiveProcesses := config.Backup.Parallel - len(activeProcesses)
				mu.Unlock()
				LogInfo("📊 [PARALLEL-STATUS] Worker-%d starting %s, %d/%d processes currently active",
					workerID, dbName, currentActiveProcesses, config.Backup.Parallel)

				// Memory management (same as full backup)
				if shouldRestartMySQL(config) {
					LogWarn("⚠️ [MEMORY] Memory threshold exceeded (%.2f%% > %d%%), scheduling MySQL restart",
						getCurrentMemoryUsage(), config.Backup.MaxMemoryThreshold)

					LogDebug("🔓 [WORKER-%d] Releasing process slot temporarily for MySQL restart", workerID)
					<-activeProcesses

					mu.Lock()
					if mysqlRestartInProgress {
						mu.Unlock()
						LogInfo("⏳ [WORKER-%d] MySQL restart already in progress, waiting...", workerID)
						for {
							time.Sleep(5 * time.Second)
							mu.Lock()
							if !mysqlRestartInProgress {
								mu.Unlock()
								break
							}
							mu.Unlock()
						}

						LogDebug("🔒 [WORKER-%d] Re-acquiring process slot after MySQL restart", workerID)
						activeProcesses <- struct{}{}
					} else {
						mysqlRestartInProgress = true
						mu.Unlock()

						LogInfo("⏳ [MEMORY] Waiting for all active backup processes to complete before MySQL restart...")
						for j := 0; j < config.Backup.Parallel; j++ {
							activeProcesses <- struct{}{}
						}
						for j := 0; j < config.Backup.Parallel; j++ {
							<-activeProcesses
						}
						LogInfo("✅ [MEMORY] All active processes completed, proceeding with MySQL restart")

						LogInfo("🔄 [MYSQL-RESTART] Starting MySQL service restart with monitoring")
						if !restartMySQLServiceWithMonitoring(config, &abortBackup) {
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
						LogInfo("✅ [MYSQL-RESTART] MySQL service restart completed successfully")

						mu.Lock()
						mysqlRestartInProgress = false
						mu.Unlock()

						LogDebug("🔒 [WORKER-%d] Re-acquiring process slot after MySQL restart", workerID)
						activeProcesses <- struct{}{}
					}
				} else {
					LogDebug("✅ [MEMORY] Memory usage OK (%.2f%% <= %d%%), proceeding with backup",
						getCurrentMemoryUsage(), config.Backup.MaxMemoryThreshold)
				}

				// Check for global abort before starting actual backup
				if CheckGlobalBackupAbort() {
					LogWarn("⚠️ [WORKER-%d] Incremental backup aborted before starting actual backup of %s", workerID, dbName)
					<-activeProcesses
					return
				}

				LogInfo("💾 [BACKUP] Worker-%d: Starting mariadb-binlog incremental backup for database: %s", workerID, dbName)
				backupResult := executeIncrementalDatabaseBackup(dbName, request.JobID, config, latestBackupTime)

				LogDebug("🔒 [WORKER-%d] Setting job status to 'running' for %s", workerID, dbName)
				mu.Lock()
				if backupResult.Success {
					totalSizeKB += backupResult.SizeKB
					totalInc++

					// Add database disk size to total
					if dbSizeBytes, err := getDatabaseSize(dbName, config); err == nil {
						totalDiskSizeKB += int(dbSizeBytes / 1024)
						LogDebug("📏 [DISK-SIZE] Added %d KB disk size for %s to total", int(dbSizeBytes/1024), dbName)
					} else {
						LogWarn("⚠️ [DISK-SIZE] Failed to get disk size for %s: %v", dbName, err)
					}

					LogInfo("✅ [BACKUP-SUCCESS] Worker-%d: Incremental backup completed for %s - Size: %d KB, File: %s",
						workerID, dbName, backupResult.SizeKB, backupResult.FilePath)
				} else {
					totalFailed++
					LogError("❌ [BACKUP-FAILED] Worker-%d: Incremental backup failed for %s: %s",
						workerID, dbName, backupResult.ErrorMessage)
				}

				LogDebug("📊 [SUMMARY] Updating backup summary - JobID: %s, Size: %d KB, Disk Size: %d KB, Inc: %d, Failed: %d",
					request.JobID, totalSizeKB, totalDiskSizeKB, totalInc, totalFailed)
				UpdateBackupSummary(request.JobID, totalSizeKB, totalDiskSizeKB, 0, totalInc, totalFailed)
				mu.Unlock()

				<-activeProcesses
				LogDebug("✅ [WORKER-%d] Process slot released, ready for next database", workerID)

				mu.Lock()
				currentActiveProcesses = config.Backup.Parallel - len(activeProcesses)
				mu.Unlock()
				LogInfo("📊 [PARALLEL-STATUS] Worker-%d completed %s, %d/%d processes currently active",
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
	CompleteBackupSummary(request.JobID, config)

	LogInfo("🎉 [BACKUP-COMPLETE] Incremental backup job %s completed - Total: %d, Successful: %d, Failed: %d, Total Size: %d KB, Total Disk Size: %d KB",
		request.JobID, len(request.Databases), totalInc, totalFailed, totalSizeKB, totalDiskSizeKB)

	LogInfo("📊 [PARALLEL-SUMMARY] Incremental backup job %s used %d parallel workers, processed %d databases with %d%% success rate",
		request.JobID, config.Backup.Parallel, len(request.Databases),
		int(float64(totalInc)/float64(len(request.Databases))*100))
}

// IncrementalDatabaseBackupResult represents the result of a single incremental database backup
type IncrementalDatabaseBackupResult struct {
	Success      bool
	SizeKB       int
	FilePath     string
	ErrorMessage string
}

// executeIncrementalDatabaseBackup executes incremental backup for a single database using mariadb-binlog
func executeIncrementalDatabaseBackup(dbName, jobID string, config *Config, startTime time.Time) IncrementalDatabaseBackupResult {
	LogDebug("🏗️ [INC-BACKUP-SETUP] Setting up incremental backup for database: %s, JobID: %s", dbName, jobID)

	backupDir := filepath.Join(config.Backup.BackupDir, dbName)
	err := os.MkdirAll(backupDir, 0755)
	if err != nil {
		LogError("❌ [DIRECTORY-ERROR] Failed to create backup directory %s: %v", backupDir, err)

		// Update job status to failed
		updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", fmt.Sprintf("Failed to create backup directory: %v", err))
		if updateErr != nil {
			LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
		}

		return IncrementalDatabaseBackupResult{
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

	tempFileName := fmt.Sprintf("temp_inc_%s_%s%s", dbName, time.Now().Format("20060102_150405.000000"), extension)
	tempFilePath := filepath.Join(backupDir, tempFileName)
	cmd := buildMariadbBinlogCommand(dbName, tempFilePath, config, startTime)

	backupStartTime := startTime // Use the calculated start time, not current time
	LogInfo("🚀 [EXECUTE] Starting mariadb-binlog process for %s", dbName)
	LogDebug("📅 [TIMING] Backup start time set to: %s", backupStartTime.Format("2006-01-02 15:04:05.000000"))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		LogError("❌ [EXECUTE-ERROR] Failed to get stdout pipe for %s: %v", dbName, err)
		return IncrementalDatabaseBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to get stdout pipe: %v", err),
		}
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		LogError("❌ [EXECUTE-ERROR] Failed to start incremental backup command for %s: %v", dbName, err)

		// Update job status to failed
		updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", fmt.Sprintf("Failed to start incremental backup command: %v", err))
		if updateErr != nil {
			LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
		}

		return IncrementalDatabaseBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to start incremental backup command: %v", err),
		}
	}
	LogDebug("✅ [EXECUTE] Mariadb-binlog process started successfully (PID: %d)", cmd.Process.Pid)

	// Start monitoring progress and writing to file
	go func() {
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
		LogDebug("📊 [INC-BACKUP-MONITOR] Starting incremental backup monitoring for %s", dbName)

		scanner := bufio.NewScanner(stdout)
		// Increase buffer size to handle very long lines (up to 64MB)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 64*1024*1024)

		processedEvents := 0

		for scanner.Scan() {
			line := scanner.Text()

			// Write line to file
			_, writeErr := outputFile.WriteString(line + "\n")
			if writeErr != nil {
				LogError("❌ [BACKUP-ERROR] Failed to write to incremental backup file for %s: %v", dbName, writeErr)
				updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", fmt.Sprintf("File write error: %v", writeErr))
				if updateErr != nil {
					LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
				}
				stdout.Close()
				return
			}

			// Count binlog events for logging (no progress tracking)
			if strings.Contains(line, "### ") || strings.Contains(line, "INSERT") || strings.Contains(line, "UPDATE") || strings.Contains(line, "DELETE") {
				processedEvents++
			}
		}

		// Check for scanner errors
		if err := scanner.Err(); err != nil {
			var errorMessage string
			if strings.Contains(err.Error(), "token too long") {
				errorMessage = "Scanner error: Line too long (max 64MB). This usually happens with large binlog events."
				LogError("❌ [INC-BACKUP-MONITOR] Scanner error for %s: %s", dbName, errorMessage)
			} else {
				errorMessage = fmt.Sprintf("Scanner error: %v", err)
				LogError("❌ [INC-BACKUP-MONITOR] Scanner error for %s: %v", dbName, err)
			}

			// Update job status to failed and stop the backup process
			LogError("❌ [BACKUP-ERROR] Incremental backup failed for %s due to scanner error, updating job status to failed", dbName)
			updateErr := CompleteBackupJob(jobID, dbName, false, 0, "", errorMessage)
			if updateErr != nil {
				LogError("❌ [SQLITE-ERROR] Failed to update job status to failed for %s: %v", dbName, updateErr)
			}

			// Close the stdout pipe to stop the mariadb-binlog process
			stdout.Close()
			return
		}

		LogInfo("✅ [INC-BACKUP-MONITOR] Incremental backup monitoring completed for %s: %d events processed",
			dbName, processedEvents)
	}()

	// Generate final filename with exact backup start time (will be updated to end time later)
	timestamp := backupStartTime.Format("20060102_150405.000000")
	backupFileName := fmt.Sprintf("inc_%s_%s%s", dbName, timestamp, extension)
	backupFilePath := filepath.Join(backupDir, backupFileName)
	LogDebug("📝 [FILENAME] Generated initial incremental backup filename: %s", backupFileName)
	LogDebug("📁 [FILEPATH] Initial incremental backup file path: %s", backupFilePath)

	// Set progress to 10% for incremental backup (no detailed progress tracking)
	LogDebug("📊 [PROGRESS] Setting progress to 10%% for incremental backup phase of %s", dbName)
	UpdateBackupJobProgress(jobID, dbName, 10)

	// Wait for command to complete
	LogDebug("⏳ [EXECUTE] Waiting for mariadb-binlog process to complete for %s", dbName)
	err = cmd.Wait()
	duration := time.Since(backupStartTime)

	// Close the stdout pipe
	stdout.Close()

	if err != nil {
		var errorMessage string
		if exitError, ok := err.(*exec.ExitError); ok {
			switch exitError.ExitCode() {
			case 1:
				errorMessage = "mariadb-binlog: General error (check binlog files and permissions)"
			case 2:
				errorMessage = "mariadb-binlog: Misuse of shell builtins"
			default:
				errorMessage = fmt.Sprintf("mariadb-binlog failed with exit code %d: %v", exitError.ExitCode(), err)
			}
		} else {
			errorMessage = err.Error()
		}

		LogError("❌ [EXECUTE-ERROR] Incremental backup command failed for %s after %v: %s", dbName, duration, errorMessage)

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

		return IncrementalDatabaseBackupResult{
			Success:      false,
			ErrorMessage: errorMessage,
		}
	}
	LogInfo("✅ [EXECUTE] Mariadb-binlog process completed successfully for %s in %v", dbName, duration)

	// Generate final filename with backup end time
	backupEndTime := time.Now()
	endTimestamp := backupEndTime.Format("20060102_150405.000000")
	finalBackupFileName := fmt.Sprintf("inc_%s_%s%s", dbName, endTimestamp, extension)
	finalBackupFilePath := filepath.Join(backupDir, finalBackupFileName)
	LogDebug("📝 [FILENAME] Generated final incremental backup filename with end time: %s", finalBackupFileName)
	LogDebug("📁 [FILEPATH] Final incremental backup file path: %s", finalBackupFilePath)

	// Add backup metadata comment to the end of the file
	LogDebug("📝 [METADATA] Adding backup metadata comment to file")
	err = addBackupMetadataComment(tempFilePath, backupStartTime, backupEndTime, dbName)
	if err != nil {
		LogWarn("⚠️ [METADATA] Failed to add backup metadata comment: %v", err)
		// Continue with backup even if metadata comment fails
	}

	// Rename temporary file to final filename with retry logic
	LogDebug("📁 [RENAME] Renaming temporary file to final filename")

	// Small delay to ensure file handles are released (especially on Windows)
	time.Sleep(100 * time.Millisecond)

	finalFilePath := renameWithRetry(tempFilePath, finalBackupFilePath, dbName)

	// Check if rename was successful
	renameSuccessful := (finalFilePath == finalBackupFilePath)
	var errorMessage string
	if !renameSuccessful {
		errorMessage = fmt.Sprintf("Incremental backup completed but file rename failed. Using temporary file: %s", tempFilePath)
		LogWarn("⚠️ [RENAME-WARNING] %s", errorMessage)
	}

	fileInfo, err := os.Stat(finalFilePath)
	var sizeKB int
	if err == nil {
		sizeKB = int(fileInfo.Size() / 1024)
	} else {
		LogWarn("⚠️ [SIZE] Failed to get file size for %s: %v", finalFilePath, err)
		if errorMessage == "" {
			errorMessage = fmt.Sprintf("Incremental backup completed but failed to get file size: %v", err)
		}
	}

	// Determine final status based on rename success and file access
	backupSuccess := renameSuccessful && err == nil
	if !backupSuccess && errorMessage == "" {
		errorMessage = "Incremental backup completed with unknown issues"
	}

	// Update job status based on actual result
	LogDebug("💾 [SQLITE] Updating job status for %s - Success: %v, Error: %s", dbName, backupSuccess, errorMessage)
	err = CompleteBackupJob(jobID, dbName, backupSuccess, sizeKB, finalFilePath, errorMessage)
	if err != nil {
		LogError("❌ [SQLITE-ERROR] Failed to update job status for %s: %v", dbName, err)
		return IncrementalDatabaseBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Incremental backup completed but failed to update database: %v", err),
		}
	}

	if backupSuccess {
		LogInfo("🎉 [INC-BACKUP-SUCCESS] Incremental database backup completed successfully - DB: %s, Size: %d KB, Duration: %v, File: %s",
			dbName, sizeKB, duration, finalFilePath)
	} else {
		LogWarn("⚠️ [INC-BACKUP-PARTIAL] Incremental database backup completed with issues - DB: %s, Size: %d KB, Duration: %v, File: %s, Error: %s",
			dbName, sizeKB, duration, finalFilePath, errorMessage)
	}

	return IncrementalDatabaseBackupResult{
		Success:      backupSuccess,
		SizeKB:       sizeKB,
		FilePath:     finalFilePath,
		ErrorMessage: errorMessage,
	}
}

// buildMariadbBinlogCommand builds the mariadb-binlog command with all necessary arguments
func buildMariadbBinlogCommand(dbName, outputPath string, config *Config, startTime time.Time) *exec.Cmd {
	// Check if we're on Windows
	isWindows := runtime.GOOS == "windows"

	// Build the command based on platform and requirements
	var cmd *exec.Cmd

	if isWindows {
		// Windows: Simple mariadb-binlog command (no nice, no gzip)
		cmd = buildWindowsBinlogCommand(dbName, outputPath, config, startTime)
	} else {
		// Linux: Full command with nice and optional gzip
		cmd = buildLinuxBinlogCommand(dbName, outputPath, config, startTime)
	}

	return cmd
}

// buildWindowsBinlogCommand builds command for Windows (no nice, no gzip)
func buildWindowsBinlogCommand(dbName, outputPath string, config *Config, startTime time.Time) *exec.Cmd {
	// Start with the binary path
	cmd := exec.Command(config.Database.BinaryBinLog)

	// Add connection arguments
	connArgs := buildMySQLConnectionArgs(config)
	cmd.Args = append(cmd.Args, connArgs...)

	// Add database filter
	cmd.Args = append(cmd.Args, "--database="+dbName)

	// Add start datetime
	startTimeStr := startTime.Format("2006-01-02 15:04:05")
	cmd.Args = append(cmd.Args, "--start-datetime="+startTimeStr)

	// Add binlog files from config
	binlogFiles := getBinlogFiles(config)
	if len(binlogFiles) > 0 {
		cmd.Args = append(cmd.Args, binlogFiles...)
	}

	// Add binlog options from config
	if config.Backup.MariadbBinlogOptions != "" {
		options := strings.Fields(config.Backup.MariadbBinlogOptions)
		cmd.Args = append(cmd.Args, options...)
		LogDebug("Added binlog options from config: %s", config.Backup.MariadbBinlogOptions)
	} else {
		// Default options if config is empty
		cmd.Args = append(cmd.Args, "--verbose")
		cmd.Args = append(cmd.Args, "--base64-output=DECODE-ROWS")
		LogDebug("Using default binlog options: --verbose --base64-output=DECODE-ROWS")
	}

	// Set output file - let the command handle file creation
	cmd.Stdout = nil // Will be set to the file when command starts

	LogInfo("Windows mariadb-binlog command built for %s: %s", dbName, strings.Join(cmd.Args, " "))
	return cmd
}

// buildLinuxBinlogCommand builds command for Linux with nice and optional gzip
func buildLinuxBinlogCommand(dbName, outputPath string, config *Config, startTime time.Time) *exec.Cmd {
	var cmd *exec.Cmd

	if config.Backup.CompressionLevel > 0 {
		// Use shell command with nice and gzip
		cmd = buildLinuxCompressedBinlogCommand(dbName, outputPath, config, startTime)
	} else {
		// Use shell command with just nice
		cmd = buildLinuxUncompressedBinlogCommand(dbName, outputPath, config, startTime)
	}

	return cmd
}

// buildLinuxCompressedBinlogCommand builds Linux command with nice and gzip
func buildLinuxCompressedBinlogCommand(dbName, outputPath string, config *Config, startTime time.Time) *exec.Cmd {
	// Build mariadb-binlog command
	binlogCmd := buildMariadbBinlogArgs(dbName, config, startTime)

	// Create shell command: nice -n $level mariadb-binlog ... | gzip -$level > output.gz
	niceLevel := config.Backup.NiceLevel
	compressionLevel := config.Backup.CompressionLevel

	shellCmd := fmt.Sprintf("nice -n %d %s | gzip -%d > %s",
		niceLevel,
		strings.Join(binlogCmd, " "),
		compressionLevel,
		outputPath)

	cmd := exec.Command("sh", "-c", shellCmd)

	LogInfo("Linux compressed mariadb-binlog command built for %s: %s", dbName, shellCmd)
	return cmd
}

// buildLinuxUncompressedBinlogCommand builds Linux command with just nice
func buildLinuxUncompressedBinlogCommand(dbName, outputPath string, config *Config, startTime time.Time) *exec.Cmd {
	// Build mariadb-binlog command
	binlogCmd := buildMariadbBinlogArgs(dbName, config, startTime)

	// Create shell command: nice -n $level mariadb-binlog ... > output.sql
	niceLevel := config.Backup.NiceLevel

	shellCmd := fmt.Sprintf("nice -n %d %s > %s",
		niceLevel,
		strings.Join(binlogCmd, " "),
		outputPath)

	cmd := exec.Command("sh", "-c", shellCmd)

	LogInfo("Linux uncompressed mariadb-binlog command built for %s: %s", dbName, shellCmd)
	return cmd
}

// buildMariadbBinlogArgs builds the mariadb-binlog arguments without executing
func buildMariadbBinlogArgs(dbName string, config *Config, startTime time.Time) []string {
	var args []string

	// Add binary path
	args = append(args, config.Database.BinaryBinLog)

	// Add connection arguments
	connArgs := buildMySQLConnectionArgs(config)
	args = append(args, connArgs...)

	// Add database filter
	args = append(args, "--database="+dbName)

	// Add start datetime
	startTimeStr := startTime.Format("2006-01-02 15:04:05")
	args = append(args, "--start-datetime="+startTimeStr)

	// Add binlog files from config
	binlogFiles := getBinlogFiles(config)
	if len(binlogFiles) > 0 {
		args = append(args, binlogFiles...)
	}

	// Add binlog options from config
	if config.Backup.MariadbBinlogOptions != "" {
		options := strings.Fields(config.Backup.MariadbBinlogOptions)
		args = append(args, options...)
		LogDebug("Added binlog options from config: %s", config.Backup.MariadbBinlogOptions)
	} else {
		// Default options if config is empty
		args = append(args, "--verbose")
		args = append(args, "--base64-output=DECODE-ROWS")
		LogDebug("Using default binlog options: --verbose --base64-output=DECODE-ROWS")
	}

	return args
}

// getBinlogFiles gets the list of binlog files by querying the database for log_bin_basename
func getBinlogFiles(config *Config) []string {
	// Get binlog basename from database
	dsn, err := buildMySQLDSN(config)
	if err != nil {
		LogWarn("⚠️ [BINLOG-FILES] Failed to build DSN: %v", err)
		return []string{}
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		LogWarn("⚠️ [BINLOG-FILES] Failed to connect to database: %v", err)
		return []string{}
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		LogWarn("⚠️ [BINLOG-FILES] Database connection failed: %v", err)
		return []string{}
	}

	var binlogBasename string
	err = db.QueryRow("SHOW VARIABLES LIKE 'log_bin_basename'").Scan(&binlogBasename, &binlogBasename)
	if err != nil {
		LogWarn("⚠️ [BINLOG-FILES] Failed to query log_bin_basename: %v", err)
		return []string{}
	}

	// Generate binlog path pattern with .[0-9]* for all binlog files
	binlogPath := binlogBasename + ".[0-9]*"
	LogDebug("📁 [BINLOG-FILES] Using binlog path pattern: %s", binlogPath)

	matches, err := filepath.Glob(binlogPath)
	if err != nil {
		LogWarn("⚠️ [BINLOG-FILES] Error searching for binlog files with pattern %s: %v", binlogPath, err)
		return []string{}
	}

	if len(matches) == 0 {
		LogWarn("⚠️ [BINLOG-FILES] No binlog files found matching pattern: %s", binlogPath)
		return []string{}
	}

	LogDebug("📁 [BINLOG-FILES] Found %d binlog files: %v", len(matches), matches)
	return matches
}

// addBackupMetadataComment adds backup metadata comment to the end of the backup file
func addBackupMetadataComment(filePath string, startTime, endTime time.Time, dbName string) error {
	LogDebug("📝 [METADATA] Adding backup metadata comment to %s", filePath)

	// Open file in append mode
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for metadata comment: %v", err)
	}
	defer file.Close()

	// Calculate duration
	duration := endTime.Sub(startTime)

	// Create metadata comment
	metadataComment := fmt.Sprintf(`

-- =============================================
-- MariaDB Incremental Backup Metadata
-- =============================================
-- Database: %s
-- Backup Start Time: %s
-- Backup End Time: %s
-- Backup Duration: %v
-- Backup Type: Incremental (mariadb-binlog)
-- Generated by: MariaDB Backup Tool
-- =============================================
`, dbName,
		startTime.Format("2006-01-02 15:04:05.000000"),
		endTime.Format("2006-01-02 15:04:05.000000"),
		duration)

	// Write metadata comment to file
	_, err = file.WriteString(metadataComment)
	if err != nil {
		return fmt.Errorf("failed to write metadata comment: %v", err)
	}

	LogDebug("✅ [METADATA] Successfully added backup metadata comment")
	return nil
}
