package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Database     DatabaseConfig     `json:"database"`
	Backup       BackupConfig       `json:"backup"`
	Web          WebConfig          `json:"web"`
	Logging      LoggingConfig      `json:"logging"`
	Notification NotificationConfig `json:"notification"`
}

type DatabaseConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Socket       string `json:"socket"`
	BinaryDump   string `json:"binary_dump"`
	BinaryCheck  string `json:"binary_check"`
	BinaryBinLog string `json:"binary_binlog"`
}

type BackupConfig struct {
	BackupDir            string   `json:"backup_dir"`
	RetentionBackups     int      `json:"retention_backups"`
	Parallel             int      `json:"parallel"`
	FullBackupInterval   int      `json:"full_backup_interval"`
	BackupIntervalHours  int      `json:"backup_interval_hours"`
	BackupStartTime      string   `json:"backup_start_time"`
	CompressionLevel     int      `json:"compression_level"`
	NiceLevel            int      `json:"nice_level"`
	IgnoreDbs            []string `json:"ignore_dbs"`
	DefaultBackupMode    string   `json:"default_backup_mode"`
	OptimizeTables       bool     `json:"optimize_tables"`
	MaxMemoryThreshold   int      `json:"max_memory_threshold"`
	MaxMemoryPerProcess  string   `json:"max_memory_per_process"`
	CreateTableInfo      bool     `json:"create_table_info"`
	MysqldumpOptions     string   `json:"mysqldump_options"`
	MariadbCheckOptions  string   `json:"mariadb_check_options"`
	MariadbBinlogOptions string   `json:"mariadb_binlog_options"`
}

type WebConfig struct {
	Port         int    `json:"port"`
	AuthUser     string `json:"auth_user"`
	AuthPassHash string `json:"auth_pass_hash"`
	SSLEnabled   bool   `json:"ssl_enabled"`
	SSLCertFile  string `json:"ssl_cert_file"`
	SSLKeyFile   string `json:"ssl_key_file"`
}

type LoggingConfig struct {
	LogDir        string `json:"log_dir"`
	RetentionLogs int    `json:"retention_logs"`
}

type NotificationConfig struct {
	SlackWebhookURL string `json:"slack_webhook_url"`
}

func loadConfig(configFile string) (*Config, error) {
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		LogInfo("Config file %s not found, creating default configuration", configFile)
		return createDefaultConfig(configFile)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	return &config, nil
}

func createDefaultConfig(configFile string) (*Config, error) {
	config := &Config{
		Database: DatabaseConfig{
			Host:         "127.0.0.1",
			Port:         3306,
			Username:     "root",
			Password:     "",
			Socket:       "/var/run/mysqld/mysqld.sock",
			BinaryDump:   "/usr/bin/mariadb-dump",
			BinaryCheck:  "/usr/bin/mariadb-check",
			BinaryBinLog: "/usr/bin/mariadb-binlog",
		},
		Backup: BackupConfig{
			BackupDir:           "/etc/mariadb-backup-tool/backups",
			RetentionBackups:    30,
			Parallel:            8,
			FullBackupInterval:  7,
			BackupIntervalHours: 0,
			BackupStartTime:     "09:00",
			CompressionLevel:    6,
			NiceLevel:           15,
			IgnoreDbs: []string{
				"information_schema",
				"performance_schema",
				"mysql",
				"sys",
			},
			DefaultBackupMode:    "auto",
			OptimizeTables:       false,
			MaxMemoryThreshold:   80,
			MaxMemoryPerProcess:  "256M",
			CreateTableInfo:      true,
			MysqldumpOptions:     "--quick --lock-tables=false --skip-lock-tables --single-transaction --no-autocommit --net_buffer_length=16k --skip-triggers --skip-routines --skip-events --default-character-set=utf8mb4 --compact --extended-insert --compress --opt --hex-blob --disable-keys",
			MariadbCheckOptions:  "--auto-repair --optimize",
			MariadbBinlogOptions: "--verbose --base64-output=DECODE-ROWS --short-form",
		},
		Web: WebConfig{
			Port:         8080,
			AuthUser:     "admin",
			AuthPassHash: "$2a$12$s0o87ZWJjjG5KG7pM/w5MO9jx5d/kzVYycSmlMw4B4N0m/4FhdWO2", //admin
			SSLEnabled:   false,
			SSLCertFile:  "server.crt",
			SSLKeyFile:   "server.key",
		},
		Logging: LoggingConfig{
			LogDir:        "/etc/mariadb-backup-tool/logs",
			RetentionLogs: 30,
		},
		Notification: NotificationConfig{
			SlackWebhookURL: "",
		},
	}

	if err := saveConfig(config, configFile); err != nil {
		return nil, fmt.Errorf("failed to save default config: %v", err)
	}

	LogInfo("Default configuration created at %s", configFile)
	return config, nil
}

func saveConfig(config *Config, configFile string) error {
	dir := filepath.Dir(configFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}
