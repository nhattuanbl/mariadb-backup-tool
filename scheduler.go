package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Scheduler represents the backup scheduler
type Scheduler struct {
	config    *Config
	stopChan  chan bool
	isRunning bool
	lastRun   time.Time
	nextRun   time.Time
}

var backupScheduler *Scheduler

// CalculateNextBackupTime calculates the next scheduled backup time based on start time and interval
// This function is shared between scheduler and UI to ensure consistency
func CalculateNextBackupTime(startTime string, intervalHours int) string {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Parse start time (format: "HH:MM")
	startTimeParts := strings.Split(startTime, ":")
	if len(startTimeParts) != 2 {
		return ""
	}

	startHour, err := strconv.Atoi(startTimeParts[0])
	if err != nil {
		return ""
	}
	startMinute, err := strconv.Atoi(startTimeParts[1])
	if err != nil {
		return ""
	}

	// Create start datetime for today
	startDateTime := today.Add(time.Duration(startHour)*time.Hour + time.Duration(startMinute)*time.Minute)

	// Calculate next run time
	var nextRun time.Time

	// Handle zero interval case (scheduler disabled)
	if intervalHours == 0 {
		return "" // Return empty string to indicate scheduler is disabled
	}

	// If start time hasn't passed today, next run is today's start time
	if startDateTime.After(now) {
		nextRun = startDateTime
	} else {
		// Calculate how many intervals have passed since start time today
		timeDiff := now.Sub(startDateTime)
		intervalsPassed := int(timeDiff.Hours()) / intervalHours

		// Next run is the next interval after the last completed one
		nextRun = startDateTime.Add(time.Duration(intervalsPassed+1) * time.Duration(intervalHours) * time.Hour)

		// If the next run is still in the past (shouldn't happen), add another interval
		if nextRun.Before(now) || nextRun.Equal(now) {
			nextRun = nextRun.Add(time.Duration(intervalHours) * time.Hour)
		}
	}

	return nextRun.Format("2006-01-02 15:04:05")
}

// StartScheduler starts the backup scheduler
func StartScheduler(config *Config) {
	if backupScheduler != nil && backupScheduler.isRunning {
		LogWarn("Scheduler is already running")
		return
	}

	// Check if scheduler should be disabled (interval = 0)
	if config.Backup.BackupIntervalHours == 0 {
		LogInfo("Scheduler disabled - backup_interval_hours is set to 0")
		backupScheduler = &Scheduler{
			config:    config,
			stopChan:  make(chan bool),
			isRunning: false,
		}
		return
	}

	backupScheduler = &Scheduler{
		config:    config,
		stopChan:  make(chan bool),
		isRunning: true,
	}

	LogInfo("Starting backup scheduler...")
	LogInfo("Schedule configuration - Start time: %s, Interval: %d hours, Mode: %s",
		config.Backup.BackupStartTime,
		config.Backup.BackupIntervalHours,
		config.Backup.DefaultBackupMode)

	go backupScheduler.run()
}

// StopScheduler stops the backup scheduler
func StopScheduler() {
	if backupScheduler == nil || !backupScheduler.isRunning {
		return
	}

	LogInfo("Stopping backup scheduler...")
	backupScheduler.stopChan <- true
	backupScheduler.isRunning = false
	LogInfo("Backup scheduler stopped")
}

// IsSchedulerRunning returns whether the scheduler is currently running
func IsSchedulerRunning() bool {
	return backupScheduler != nil && backupScheduler.isRunning
}

// GetSchedulerStatus returns the current scheduler status
func GetSchedulerStatus() map[string]interface{} {
	if backupScheduler == nil {
		return map[string]interface{}{
			"running": false,
			"error":   "Scheduler not initialized",
		}
	}

	return map[string]interface{}{
		"running":  backupScheduler.isRunning,
		"last_run": backupScheduler.lastRun,
		"next_run": backupScheduler.nextRun,
		"config": map[string]interface{}{
			"start_time":     backupScheduler.config.Backup.BackupStartTime,
			"interval_hours": backupScheduler.config.Backup.BackupIntervalHours,
			"default_mode":   backupScheduler.config.Backup.DefaultBackupMode,
		},
	}
}

// run is the main scheduler loop
func (s *Scheduler) run() {
	LogInfo("Backup scheduler started successfully")

	// Calculate initial next run time
	s.nextRun = s.calculateNextRunTime()
	LogInfo("Next scheduled backup: %s", s.nextRun.Format("2006-01-02 15:04:05"))

	// Create a ticker that checks every minute
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			LogInfo("Scheduler stop signal received")
			return
		case <-ticker.C:
			s.checkAndTriggerBackup()
		}
	}
}

// checkAndTriggerBackup checks if it's time to run a backup and triggers it if needed
func (s *Scheduler) checkAndTriggerBackup() {
	now := time.Now()

	// Check if it's time for the next backup
	if now.After(s.nextRun) || now.Equal(s.nextRun) {
		LogInfo("Scheduled backup time reached: %s", s.nextRun.Format("2006-01-02 15:04:05"))

		// Trigger the backup
		s.triggerScheduledBackup()

		// Update last run time
		s.lastRun = now

		// Calculate next run time
		if s.config.Backup.BackupIntervalHours == 0 {
			// For zero interval, schedule next run in 1 minute
			s.nextRun = now.Add(1 * time.Minute)
		} else {
			s.nextRun = s.calculateNextRunTime()
		}
		LogInfo("Next scheduled backup: %s", s.nextRun.Format("2006-01-02 15:04:05"))
	}
}

// triggerScheduledBackup triggers a scheduled backup
func (s *Scheduler) triggerScheduledBackup() {
	LogInfo("Triggering scheduled backup...")

	// Reset global abort flag when starting scheduled backups
	ResetGlobalBackupAbort()

	// Get all databases (excluding ignored ones)
	databases, err := getDatabases(s.config)
	if err != nil {
		LogError("Failed to get databases for scheduled backup: %v", err)
		return
	}

	// Filter out ignored databases
	validDatabases := []string{}
	for _, db := range databases {
		if !s.isDatabaseIgnored(db) {
			validDatabases = append(validDatabases, db)
		}
	}

	if len(validDatabases) == 0 {
		LogWarn("No databases available for scheduled backup")
		return
	}

	LogInfo("Scheduled backup will process %d databases: %v", len(validDatabases), validDatabases)

	// Generate job ID
	jobID := GenerateJobID()
	LogInfo("Scheduled backup job ID: %s", jobID)

	// Route to appropriate backup function based on mode
	switch s.config.Backup.DefaultBackupMode {
	case "full":
		s.triggerFullBackup(jobID, validDatabases)
	case "incremental":
		s.triggerIncrementalBackup(jobID, validDatabases)
	case "auto":
		s.triggerAutoBackup(jobID, validDatabases)
	default:
		LogError("Unknown backup mode: %s", s.config.Backup.DefaultBackupMode)
	}

	// Run backup cleanup after scheduled backup
	go func() {
		// Wait a bit to ensure backup is complete
		time.Sleep(30 * time.Second)
		LogInfo("Running scheduled backup cleanup...")
		if err := CleanupOldBackups(s.config); err != nil {
			LogError("Scheduled backup cleanup failed: %v", err)
		}
	}()
}

// triggerFullBackup triggers a full backup
func (s *Scheduler) triggerFullBackup(jobID string, databases []string) {
	LogInfo("Starting scheduled full backup for %d databases", len(databases))

	request := BackupFullRequest{
		JobID:       jobID,
		Databases:   databases,
		BackupMode:  "scheduled-full",
		RequestedBy: "scheduler",
	}

	go StartFullBackup(request)
}

// triggerIncrementalBackup triggers an incremental backup
func (s *Scheduler) triggerIncrementalBackup(jobID string, databases []string) {
	LogInfo("Starting scheduled incremental backup for %d databases", len(databases))

	request := BackupIncRequest{
		JobID:       jobID,
		Databases:   databases,
		BackupMode:  "scheduled-inc",
		RequestedBy: "scheduler",
	}

	go StartIncBackup(request)
}

// triggerAutoBackup triggers an auto backup (determines full vs incremental)
func (s *Scheduler) triggerAutoBackup(jobID string, databases []string) {
	LogInfo("Starting scheduled auto backup for %d databases", len(databases))

	// Analyze each database to determine backup type
	fullBackupDBs := []string{}
	incBackupDBs := []string{}

	for _, dbName := range databases {
		backupType := determineBackupType(dbName, s.config)
		if backupType == "full" {
			fullBackupDBs = append(fullBackupDBs, dbName)
		} else {
			incBackupDBs = append(incBackupDBs, dbName)
		}
	}

	LogInfo("Auto mode analysis - Full backups: %d databases, Incremental backups: %d databases",
		len(fullBackupDBs), len(incBackupDBs))

	// Start full backups if any databases need full backup
	if len(fullBackupDBs) > 0 {
		fullJobID := GenerateJobID()
		fullRequest := BackupFullRequest{
			JobID:       fullJobID,
			Databases:   fullBackupDBs,
			BackupMode:  "scheduled-auto-full",
			RequestedBy: "scheduler",
		}

		go StartFullBackup(fullRequest)
		LogInfo("Started scheduled full backup for %d databases (JobID: %s)", len(fullBackupDBs), fullJobID)
	}

	// Start incremental backups if any databases need incremental backup
	if len(incBackupDBs) > 0 {
		incJobID := GenerateJobID()
		incRequest := BackupIncRequest{
			JobID:       incJobID,
			Databases:   incBackupDBs,
			BackupMode:  "scheduled-auto-inc",
			RequestedBy: "scheduler",
		}

		go StartIncBackup(incRequest)
		LogInfo("Started scheduled incremental backup for %d databases (JobID: %s)", len(incBackupDBs), incJobID)
	}
}

// calculateNextRunTime calculates the next scheduled backup time
func (s *Scheduler) calculateNextRunTime() time.Time {
	// Use the shared calculation function
	nextRunStr := CalculateNextBackupTime(s.config.Backup.BackupStartTime, s.config.Backup.BackupIntervalHours)

	// If scheduler is disabled (interval = 0), return a far future time
	if nextRunStr == "" {
		return time.Now().Add(24 * 365 * time.Hour) // 1 year from now
	}

	// Parse the result back to time.Time using local timezone
	nextRun, err := time.ParseInLocation("2006-01-02 15:04:05", nextRunStr, time.Local)
	if err != nil {
		LogError("Failed to parse next run time: %v", err)
		return time.Now().Add(1 * time.Hour) // Fallback
	}

	return nextRun
}

// isDatabaseIgnored checks if a database should be ignored
func (s *Scheduler) isDatabaseIgnored(dbName string) bool {
	for _, ignoredDB := range s.config.Backup.IgnoreDbs {
		if dbName == ignoredDB {
			return true
		}
	}
	return false
}

// ReloadSchedulerConfig reloads the scheduler configuration
func ReloadSchedulerConfig(config *Config) {
	if backupScheduler == nil {
		return
	}

	LogInfo("Reloading scheduler configuration...")
	backupScheduler.config = config

	// Recalculate next run time with new config
	backupScheduler.nextRun = backupScheduler.calculateNextRunTime()
	LogInfo("Scheduler configuration reloaded. Next backup: %s",
		backupScheduler.nextRun.Format("2006-01-02 15:04:05"))
}

// CleanupOldBackups removes backup files older than the retention period
func CleanupOldBackups(config *Config) error {
	retentionDays := config.Backup.RetentionBackups
	if retentionDays <= 0 {
		LogInfo("Backup retention disabled (retention_days = %d)", retentionDays)
		return nil // No cleanup needed
	}

	LogInfo("Starting backup cleanup - retention period: %d days", retentionDays)
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)

	// Get all databases from backup directory
	backupDir := config.Backup.BackupDir
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %v", err)
	}

	var allDeletedFiles []string
	totalDeletedFiles := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		databaseName := entry.Name()
		databaseDir := filepath.Join(backupDir, databaseName)

		// Cleanup backups for this database
		deletedCount, deletedFiles, err := cleanupDatabaseBackups(databaseName, databaseDir, cutoffDate)
		if err != nil {
			LogError("Failed to cleanup backups for database %s: %v", databaseName, err)
			continue
		}

		totalDeletedFiles += deletedCount
		allDeletedFiles = append(allDeletedFiles, deletedFiles...)
		if deletedCount > 0 {
			LogInfo("Cleaned up %d backup files for database %s", deletedCount, databaseName)
		}
	}

	if totalDeletedFiles > 0 {
		LogInfo("Backup cleanup completed - removed %d files older than %d days", totalDeletedFiles, retentionDays)
		// Create deletion log
		createDeletionLog(allDeletedFiles, "retention_cleanup")
	} else {
		LogInfo("No backup files found older than %d days", retentionDays)
	}

	return nil
}

// cleanupDatabaseBackups cleans up old backup files for a specific database
func cleanupDatabaseBackups(databaseName, databaseDir string, cutoffDate time.Time) (int, []string, error) {
	// Get all backup files for this database
	allFiles, err := getAllBackupFiles(databaseDir, databaseName)
	if err != nil {
		return 0, nil, err
	}

	if len(allFiles) == 0 {
		return 0, nil, nil
	}

	// Group files by full/incremental relationships (same logic as GetDatabaseBackupFiles)
	groups := groupBackupFiles(allFiles)

	deletedCount := 0
	var deletedFiles []string

	// Check each group and delete if the full backup is older than cutoff
	for _, group := range groups {
		fullBackup := group["full_backup"].(map[string]interface{})
		fullBackupTime := fullBackup["timestamp"].(time.Time)

		// If the full backup is older than cutoff, delete the entire group
		if fullBackupTime.Before(cutoffDate) {
			// Delete full backup
			fullPath := fullBackup["file_path"].(string)
			if err := os.Remove(fullPath); err != nil {
				LogError("Failed to delete full backup %s: %v", fullPath, err)
			} else {
				deletedCount++
				deletedFiles = append(deletedFiles, fullPath)
				LogDebug("Deleted full backup: %s", fullPath)
			}

			// Delete all incremental backups in this group
			incrementalBackups := group["incremental_backups"].([]map[string]interface{})
			for _, incBackup := range incrementalBackups {
				incPath := incBackup["file_path"].(string)
				if err := os.Remove(incPath); err != nil {
					LogError("Failed to delete incremental backup %s: %v", incPath, err)
				} else {
					deletedCount++
					deletedFiles = append(deletedFiles, incPath)
					LogDebug("Deleted incremental backup: %s", incPath)
				}
			}

			LogInfo("Deleted backup group for %s (1 full + %d incremental backups)",
				databaseName, len(incrementalBackups))
		}
	}

	return deletedCount, deletedFiles, nil
}

// getAllBackupFiles gets all backup files for a database directory
func getAllBackupFiles(databaseDir, databaseName string) ([]map[string]interface{}, error) {
	var allFiles []map[string]interface{}

	// Look for full backup files (.sql and .gz)
	fullPatterns := []string{
		fmt.Sprintf("full_%s_*.sql", databaseName),
		fmt.Sprintf("full_%s_*.gz", databaseName),
	}

	for _, pattern := range fullPatterns {
		matches, err := filepath.Glob(filepath.Join(databaseDir, pattern))
		if err != nil {
			LogWarn("Error searching for full backup files for %s: %v", databaseName, err)
			continue
		}

		for _, match := range matches {
			fileInfo, err := os.Stat(match)
			if err != nil {
				continue
			}

			fileName := filepath.Base(match)
			timestamp := parseTimestampFromFilename(fileName)

			allFiles = append(allFiles, map[string]interface{}{
				"file_path":   match,
				"file_name":   fileName,
				"file_size":   fileInfo.Size(),
				"backup_type": "full",
				"timestamp":   timestamp,
				"modified_at": fileInfo.ModTime(),
			})
		}
	}

	// Look for incremental backup files (.sql and .gz)
	incPatterns := []string{
		fmt.Sprintf("inc_%s_*.sql", databaseName),
		fmt.Sprintf("inc_%s_*.gz", databaseName),
	}

	for _, pattern := range incPatterns {
		matches, err := filepath.Glob(filepath.Join(databaseDir, pattern))
		if err != nil {
			LogWarn("Error searching for incremental backup files for %s: %v", databaseName, err)
			continue
		}

		for _, match := range matches {
			fileInfo, err := os.Stat(match)
			if err != nil {
				continue
			}

			fileName := filepath.Base(match)
			timestamp := parseTimestampFromFilename(fileName)

			allFiles = append(allFiles, map[string]interface{}{
				"file_path":   match,
				"file_name":   fileName,
				"file_size":   fileInfo.Size(),
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

	return allFiles, nil
}

// groupBackupFiles groups backup files by full/incremental relationships
func groupBackupFiles(allFiles []map[string]interface{}) []map[string]interface{} {
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
		groups = append(groups, map[string]interface{}{
			"group_type":          "full_group",
			"full_backup":         currentGroup[0],  // First item is always the full backup
			"incremental_backups": currentGroup[1:], // Rest are incremental
			"total_backups":       len(currentGroup),
			"group_start_time":    currentGroup[0]["timestamp"],
		})
	}

	return groups
}
