package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackMessage represents the structure of a Slack webhook message
type SlackMessage struct {
	Text        string            `json:"text"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

// SlackAttachment represents a Slack message attachment
type SlackAttachment struct {
	Color     string       `json:"color,omitempty"`
	Title     string       `json:"title,omitempty"`
	Text      string       `json:"text,omitempty"`
	Fields    []SlackField `json:"fields,omitempty"`
	Timestamp int64        `json:"ts,omitempty"`
}

// SlackField represents a field in a Slack attachment
type SlackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// BackupSummary represents the backup summary data for Slack notification
type BackupSummary struct {
	JobID            string
	BackupMode       string
	TotalDBCount     int
	TotalFull        int
	TotalIncremental int
	TotalFailed      int
	TotalSizeKB      int
	MysqlRestartTime int // Time in seconds
	CreatedAt        time.Time
	CompletedAt      time.Time
	Duration         time.Duration
}

// SendSlackNotification sends a backup summary notification to Slack
func SendSlackNotification(webhookURL string, summary BackupSummary) error {
	if webhookURL == "" {
		LogDebug("Slack webhook URL not configured, skipping notification")
		return nil
	}

	// Determine color based on success/failure
	color := "good" // green
	if summary.TotalFailed > 0 {
		if summary.TotalFailed == summary.TotalDBCount {
			color = "danger" // red - all failed
		} else {
			color = "warning" // yellow - partial failure
		}
	}

	// Calculate success rate
	successRate := 0.0
	if summary.TotalDBCount > 0 {
		successCount := summary.TotalDBCount - summary.TotalFailed
		successRate = float64(successCount) / float64(summary.TotalDBCount) * 100
	}

	// Format file size
	sizeStr := formatFileSize(summary.TotalSizeKB)

	// Determine mode icon
	modeIcon := "📦"
	if summary.BackupMode == "auto" {
		modeIcon = "🔄"
	} else if summary.BackupMode == "full" {
		modeIcon = "📦"
	} else if summary.BackupMode == "incremental" {
		modeIcon = "📈"
	}

	// Format duration
	durationStr := formatDurationForSlack(summary.Duration)

	// Format MySQL restart time if applicable
	mysqlRestartStr := ""
	if summary.MysqlRestartTime > 0 {
		restartDuration := time.Duration(summary.MysqlRestartTime) * time.Second
		mysqlRestartStr = formatDurationForSlack(restartDuration)
	}

	// Create Slack message
	message := SlackMessage{
		Text: fmt.Sprintf("🗄️ *MariaDB Backup %s*", getBackupStatusEmoji(summary.TotalFailed, summary.TotalDBCount)),
		Attachments: []SlackAttachment{
			{
				Color:     color,
				Title:     fmt.Sprintf("Backup Job: %s", summary.JobID),
				Text:      fmt.Sprintf("Backup completed in %s", durationStr),
				Timestamp: summary.CompletedAt.Unix(),
				Fields: []SlackField{
					{
						Title: "Mode",
						Value: modeIcon,
						Short: true,
					},
					{
						Title: "Total Databases",
						Value: fmt.Sprintf("%d", summary.TotalDBCount),
						Short: true,
					},
					{
						Title: "Full Backups",
						Value: fmt.Sprintf("%d", summary.TotalFull),
						Short: true,
					},
					{
						Title: "Incremental Backups",
						Value: fmt.Sprintf("%d", summary.TotalIncremental),
						Short: true,
					},
					{
						Title: "Failed",
						Value: fmt.Sprintf("%d", summary.TotalFailed),
						Short: true,
					},
					{
						Title: "Total Size",
						Value: sizeStr,
						Short: true,
					},
					{
						Title: "Success Rate",
						Value: fmt.Sprintf("%.1f%%", successRate),
						Short: true,
					},
					{
						Title: "Duration",
						Value: durationStr,
						Short: true,
					},
				},
			},
		},
	}

	// Add MySQL restart time field if applicable
	if mysqlRestartStr != "" {
		message.Attachments[0].Fields = append(message.Attachments[0].Fields, SlackField{
			Title: "MySQL Restart Time",
			Value: mysqlRestartStr,
			Short: true,
		})
	}

	// Convert to JSON
	jsonData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack message: %v", err)
	}

	// Send HTTP POST request
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send Slack notification: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Slack webhook returned status %d", resp.StatusCode)
	}

	LogInfo("📢 Slack notification sent successfully for backup job %s", summary.JobID)
	return nil
}

// formatFileSize converts KB to human readable format
func formatFileSize(sizeKB int) string {
	if sizeKB < 1024 {
		return fmt.Sprintf("%d KB", sizeKB)
	} else if sizeKB < 1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(sizeKB)/1024)
	} else {
		return fmt.Sprintf("%.1f GB", float64(sizeKB)/(1024*1024))
	}
}

// formatDurationForSlack formats a duration into human-readable format for Slack notifications
func formatDurationForSlack(duration time.Duration) string {
	if duration < time.Minute {
		return fmt.Sprintf("%.0fs", duration.Seconds())
	} else if duration < time.Hour {
		minutes := int(duration.Minutes())
		seconds := int(duration.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		hours := int(duration.Hours())
		minutes := int(duration.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
}

// getBackupStatusEmoji returns appropriate emoji based on backup status
func getBackupStatusEmoji(failed, total int) string {
	if failed == 0 {
		return "✅ Completed Successfully"
	} else if failed == total {
		return "❌ Failed Completely"
	} else {
		return "⚠️ Completed with Issues"
	}
}

// SendSlackNotificationFromSummary sends Slack notification using backup summary data from database
func SendSlackNotificationFromSummary(webhookURL, jobID string) error {
	if webhookURL == "" {
		LogDebug("Slack webhook URL not configured, skipping notification")
		return nil
	}

	// Get backup summary from database
	summary, err := GetBackupSummaryByJobID(jobID)
	if err != nil {
		return fmt.Errorf("failed to get backup summary: %v", err)
	}

	if summary == nil {
		return fmt.Errorf("backup summary not found for job %s", jobID)
	}

	// Convert database summary to Slack summary
	slackSummary := BackupSummary{
		JobID:            summary["job_id"].(string),
		BackupMode:       summary["backup_mode"].(string),
		TotalDBCount:     summary["total_db_count"].(int),
		TotalFull:        summary["total_full"].(int),
		TotalIncremental: summary["total_incremental"].(int),
		TotalFailed:      summary["total_failed"].(int),
		TotalSizeKB:      summary["total_size_kb"].(int),
		MysqlRestartTime: summary["mysql_restart_time"].(int),
	}

	// Parse timestamps
	if createdAtStr, ok := summary["created_at"].(string); ok {
		if createdAt, err := time.Parse("2006-01-02 15:04:05", createdAtStr); err == nil {
			slackSummary.CreatedAt = createdAt
		}
	}
	if completedAtStr, ok := summary["completed_at"].(string); ok && completedAtStr != "" {
		if completedAt, err := time.Parse("2006-01-02 15:04:05", completedAtStr); err == nil {
			slackSummary.CompletedAt = completedAt
			slackSummary.Duration = completedAt.Sub(slackSummary.CreatedAt)
		}
	}

	return SendSlackNotification(webhookURL, slackSummary)
}
