// MariaDB Backup Tool - Settings Management

// Settings page functionality
function loadSettings() {
    fetch('/api/settings/load')
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                populateForm(data.config);
                
                // Update test results if available
                if (data.test_results) {
                    updateTestResultsUI(data.test_results);
                }
            } else {
                showToast('Failed to load settings: ' + data.error, 'error');
            }
        })
        .catch(error => {
            console.error('Error loading settings:', error);
            showToast('Error loading settings', 'error');
        });
    
    // Also load test results separately as fallback
    loadTestResults();
}

function loadTestResults() {
    fetch('/api/test-results')
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                updateTestResultsUI(data.results);
            }
        })
        .catch(error => {
            console.error('Error loading test results:', error);
        });
}

function updateTestResultsUI(results) {
    // Update connection test results
    const connBtn = document.getElementById('testConnectionBtn');
    const connSection = connBtn.closest('.settings-section');
    
    if (results.connection_status === 'success' || results.connection_status === 'warning') {
        connBtn.classList.add('success');
        connSection.classList.add('success');
        connBtn.querySelector('.test-icon').textContent = '✅';
        connBtn.querySelector('.test-text').textContent = 'Connected';
    } else if (results.connection_status === 'failed') {
        connBtn.classList.add('error');
        connSection.classList.add('error');
        connBtn.querySelector('.test-icon').textContent = '❌';
        connBtn.querySelector('.test-text').textContent = 'Failed';
    }
    
    // Update binary validation results
    const binaryBtn = document.getElementById('validateBinaryBtn');
    const binarySection = binaryBtn.closest('.settings-section');
    
    if (results.binary_status === 'success') {
        binaryBtn.classList.add('success');
        binarySection.classList.add('success');
        binaryBtn.querySelector('.test-icon').textContent = '✅';
        binaryBtn.querySelector('.test-text').textContent = 'Valid';
    } else if (results.binary_status === 'warning') {
        binaryBtn.classList.add('success');
        binarySection.classList.add('success');
        binaryBtn.querySelector('.test-icon').textContent = '⚠️';
        binaryBtn.querySelector('.test-text').textContent = 'Warning';
    } else if (results.binary_status === 'failed') {
        binaryBtn.classList.add('error');
        binarySection.classList.add('error');
        binaryBtn.querySelector('.test-icon').textContent = '❌';
        binaryBtn.querySelector('.test-text').textContent = 'Invalid';
    }
    
    // Update binlog format
    if (results.binlog_format) {
        const binlogFormatElement = document.getElementById('binlog_format');
        if (binlogFormatElement) {
            binlogFormatElement.value = results.binlog_format;
        }
    }
    
    // Update binlog path
    if (results.binlog_path) {
        const binlogPathElement = document.getElementById('binlog_path');
        if (binlogPathElement) {
            binlogPathElement.value = results.binlog_path;
        }
    }
}

function populateForm(config) {
    // Database settings
    const dbHostElement = document.getElementById('db_host');
    const dbPortElement = document.getElementById('db_port');
    const dbUsernameElement = document.getElementById('db_username');
    const dbPasswordElement = document.getElementById('db_password');
    const dbSocketElement = document.getElementById('db_socket');
    const binlogPathElement = document.getElementById('binlog_path');
    const binaryDumpElement = document.getElementById('binary_dump');
    const binaryCheckElement = document.getElementById('binary_check');
    const binaryBinlogElement = document.getElementById('binary_binlog');

    if (dbHostElement) dbHostElement.value = config.database.host || '';
    if (dbPortElement) dbPortElement.value = config.database.port || '';
    if (dbUsernameElement) dbUsernameElement.value = config.database.username || '';
    if (dbPasswordElement) dbPasswordElement.value = config.database.password || '';
    if (dbSocketElement) dbSocketElement.value = config.database.socket || '';
    // binlog_path is now read-only and populated from database, not from config
    if (binaryDumpElement) binaryDumpElement.value = config.database.binary_dump || '';
    if (binaryCheckElement) binaryCheckElement.value = config.database.binary_check || '';
    if (binaryBinlogElement) binaryBinlogElement.value = config.database.binary_binlog || '';

    // Backup settings
    const backupDirElement = document.getElementById('backup_dir');
    const retentionBackupsElement = document.getElementById('retention_backups');
    const parallelElement = document.getElementById('parallel');
    const fullBackupIntervalElement = document.getElementById('full_backup_interval');
    const backupIntervalHoursElement = document.getElementById('backup_interval_hours');
    const backupStartTimeElement = document.getElementById('backup_start_time');
    const compressionLevelElement = document.getElementById('compression_level');
    const niceLevelElement = document.getElementById('nice_level');
    const defaultBackupModeElement = document.getElementById('default_backup_mode');
    const optimizeTablesElement = document.getElementById('optimize_tables');
    const maxMemoryThresholdElement = document.getElementById('max_memory_threshold');
    const maxMemoryPerProcessElement = document.getElementById('max_memory_per_process');
    const createTableInfoElement = document.getElementById('create_table_info');
    const mysqldumpOptionsElement = document.getElementById('mysqldump_options');
    const mariadbCheckOptionsElement = document.getElementById('mariadb_check_options');
    const mariadbBinlogOptionsElement = document.getElementById('mariadb_binlog_options');
    const ignoreDbsElement = document.getElementById('ignore_dbs');

    if (backupDirElement) backupDirElement.value = config.backup.backup_dir || '';
    if (retentionBackupsElement) retentionBackupsElement.value = config.backup.retention_backups || '';
    if (parallelElement) parallelElement.value = config.backup.parallel || '';
    if (fullBackupIntervalElement) fullBackupIntervalElement.value = config.backup.full_backup_interval || '';
    if (backupIntervalHoursElement) backupIntervalHoursElement.value = config.backup.backup_interval_hours || '';
    if (backupStartTimeElement) backupStartTimeElement.value = config.backup.backup_start_time || '';
    if (compressionLevelElement) compressionLevelElement.value = config.backup.compression_level || '';
    if (niceLevelElement) niceLevelElement.value = config.backup.nice_level || '';
    if (defaultBackupModeElement) defaultBackupModeElement.value = config.backup.default_backup_mode || '';
    if (optimizeTablesElement) optimizeTablesElement.checked = config.backup.optimize_tables || false;
    if (maxMemoryThresholdElement) maxMemoryThresholdElement.value = config.backup.max_memory_threshold || '';
    if (maxMemoryPerProcessElement) maxMemoryPerProcessElement.value = config.backup.max_memory_per_process || '';
    if (createTableInfoElement) createTableInfoElement.checked = config.backup.create_table_info || false;
    if (mysqldumpOptionsElement) mysqldumpOptionsElement.value = config.backup.mysqldump_options || '';
    if (mariadbCheckOptionsElement) mariadbCheckOptionsElement.value = config.backup.mariadb_check_options || '';
    if (mariadbBinlogOptionsElement) mariadbBinlogOptionsElement.value = config.backup.mariadb_binlog_options || '';
    if (ignoreDbsElement) ignoreDbsElement.value = (config.backup.ignore_dbs || []).join('\n');

    // Web settings
    const webPortElement = document.getElementById('web_port');
    const authUserElement = document.getElementById('auth_user');
    const sslEnabledElement = document.getElementById('ssl_enabled');
    const sslCertFileElement = document.getElementById('ssl_cert_file');
    const sslKeyFileElement = document.getElementById('ssl_key_file');

    if (webPortElement) webPortElement.value = config.web.port || '';
    if (authUserElement) authUserElement.value = config.web.auth_user || '';
    if (sslEnabledElement) sslEnabledElement.checked = config.web.ssl_enabled || false;
    if (sslCertFileElement) sslCertFileElement.value = config.web.ssl_cert_file || '';
    if (sslKeyFileElement) sslKeyFileElement.value = config.web.ssl_key_file || '';

    // Logging settings
    const logDirElement = document.getElementById('log_dir');
    const logRetentionDaysElement = document.getElementById('log_retention_days');

    if (logDirElement) logDirElement.value = config.logging.log_dir || '';
    if (logRetentionDaysElement) logRetentionDaysElement.value = config.logging.retention_logs || '';

    // Notification settings
    const slackWebhookElement = document.getElementById('slack_webhook');
    if (slackWebhookElement) slackWebhookElement.value = config.notification.slack_webhook_url || '';

    // Update mysqldump options after populating form
    updateMysqldumpOptions();
}

function saveSettings() {
    const formData = new FormData();
    
    // Database settings
    const dbHostElement = document.getElementById('db_host');
    const dbPortElement = document.getElementById('db_port');
    const dbUsernameElement = document.getElementById('db_username');
    const dbPasswordElement = document.getElementById('db_password');
    const dbSocketElement = document.getElementById('db_socket');
    const binlogPathElement = document.getElementById('binlog_path');
    const binaryDumpElement = document.getElementById('binary_dump');
    const binaryCheckElement = document.getElementById('binary_check');
    const binaryBinlogElement = document.getElementById('binary_binlog');

    if (dbHostElement) formData.append('db_host', dbHostElement.value);
    if (dbPortElement) formData.append('db_port', dbPortElement.value);
    if (dbUsernameElement) formData.append('db_username', dbUsernameElement.value);
    if (dbPasswordElement) formData.append('db_password', dbPasswordElement.value);
    if (dbSocketElement) formData.append('db_socket', dbSocketElement.value);
    // binlog_path is now read-only and not saved to config
    if (binaryDumpElement) formData.append('binary_dump', binaryDumpElement.value);
    if (binaryCheckElement) formData.append('binary_check', binaryCheckElement.value);
    if (binaryBinlogElement) formData.append('binary_binlog', binaryBinlogElement.value);

    // Backup settings
    const backupDirElement = document.getElementById('backup_dir');
    const retentionBackupsElement = document.getElementById('retention_backups');
    const parallelElement = document.getElementById('parallel');
    const fullBackupIntervalElement = document.getElementById('full_backup_interval');
    const backupIntervalHoursElement = document.getElementById('backup_interval_hours');
    const backupStartTimeElement = document.getElementById('backup_start_time');
    const compressionLevelElement = document.getElementById('compression_level');
    const niceLevelElement = document.getElementById('nice_level');
    const defaultBackupModeElement = document.getElementById('default_backup_mode');
    const optimizeTablesElement = document.getElementById('optimize_tables');
    const maxMemoryThresholdElement = document.getElementById('max_memory_threshold');
    const maxMemoryPerProcessElement = document.getElementById('max_memory_per_process');
    const createTableInfoElement = document.getElementById('create_table_info');
    const mysqldumpOptionsElement = document.getElementById('mysqldump_options');
    const mariadbCheckOptionsElement = document.getElementById('mariadb_check_options');
    const mariadbBinlogOptionsElement = document.getElementById('mariadb_binlog_options');
    const ignoreDbsElement = document.getElementById('ignore_dbs');

    if (backupDirElement) formData.append('backup_dir', backupDirElement.value);
    if (retentionBackupsElement) formData.append('retention_backups', retentionBackupsElement.value);
    if (parallelElement) formData.append('parallel', parallelElement.value);
    if (fullBackupIntervalElement) formData.append('full_backup_interval', fullBackupIntervalElement.value);
    if (backupIntervalHoursElement) formData.append('backup_interval_hours', backupIntervalHoursElement.value);
    if (backupStartTimeElement) formData.append('backup_start_time', backupStartTimeElement.value);
    if (compressionLevelElement) formData.append('compression_level', compressionLevelElement.value);
    if (niceLevelElement) formData.append('nice_level', niceLevelElement.value);
    if (defaultBackupModeElement) formData.append('default_backup_mode', defaultBackupModeElement.value);
    if (optimizeTablesElement) formData.append('optimize_tables', optimizeTablesElement.checked ? 'on' : '');
    if (maxMemoryThresholdElement) formData.append('max_memory_threshold', maxMemoryThresholdElement.value);
    if (maxMemoryPerProcessElement) formData.append('max_memory_per_process', maxMemoryPerProcessElement.value);
    if (createTableInfoElement) formData.append('create_table_info', createTableInfoElement.checked ? 'on' : '');
    if (mysqldumpOptionsElement) formData.append('mysqldump_options', mysqldumpOptionsElement.value);
    if (mariadbCheckOptionsElement) formData.append('mariadb_check_options', mariadbCheckOptionsElement.value);
    if (mariadbBinlogOptionsElement) formData.append('mariadb_binlog_options', mariadbBinlogOptionsElement.value);
    if (ignoreDbsElement) formData.append('ignore_dbs', ignoreDbsElement.value);

    // Web settings
    const webPortElement = document.getElementById('web_port');
    const authUserElement = document.getElementById('auth_user');
    const newPasswordElement = document.getElementById('new_password');
    const sslEnabledElement = document.getElementById('ssl_enabled');
    const sslCertFileElement = document.getElementById('ssl_cert_file');
    const sslKeyFileElement = document.getElementById('ssl_key_file');

    if (webPortElement) formData.append('web_port', webPortElement.value);
    if (authUserElement) formData.append('auth_user', authUserElement.value);
    if (newPasswordElement) formData.append('new_password', newPasswordElement.value);
    if (sslEnabledElement) formData.append('ssl_enabled', sslEnabledElement.checked ? 'on' : '');
    if (sslCertFileElement) formData.append('ssl_cert_file', sslCertFileElement.value);
    if (sslKeyFileElement) formData.append('ssl_key_file', sslKeyFileElement.value);

    // Logging settings
    const logDirElement = document.getElementById('log_dir');
    const logRetentionDaysElement = document.getElementById('log_retention_days');

    if (logDirElement) formData.append('log_dir', logDirElement.value);
    if (logRetentionDaysElement) formData.append('log_retention_days', logRetentionDaysElement.value);

    // Notification settings
    const slackWebhookElement = document.getElementById('slack_webhook');
    if (slackWebhookElement) formData.append('slack_webhook', slackWebhookElement.value);

    fetch('/api/settings/save', {
        method: 'POST',
        body: formData
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            showToast('Settings saved successfully!', 'success');
            // Clear password field
            const newPasswordElement = document.getElementById('new_password');
            if (newPasswordElement) {
                newPasswordElement.value = '';
            }
        } else {
            showToast('Failed to save settings: ' + data.error, 'error');
        }
    })
    .catch(error => {
        console.error('Error saving settings:', error);
        showToast('Error saving settings', 'error');
    });
}

function resetSettings() {
    fetch('/api/settings/reset', {
        method: 'POST'
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            showToast('Settings reset to default!', 'success');
            loadSettings();
        } else {
            showToast('Failed to reset settings: ' + data.error, 'error');
        }
    })
    .catch(error => {
        console.error('Error resetting settings:', error);
        showToast('Error resetting settings', 'error');
    });
}

function updateMysqldumpOptions() {
    const createTableInfoCheckbox = document.getElementById('create_table_info');
    const maxMemoryField = document.getElementById('max_memory_per_process');
    const mysqldumpField = document.getElementById('mysqldump_options');
    const autoUpdateIndicator = document.getElementById('auto-update-indicator');

    if (!createTableInfoCheckbox || !maxMemoryField || !mysqldumpField) {
        return;
    }

    const includeTableInfo = createTableInfoCheckbox.checked;
    const memoryValue = (maxMemoryField.value || '').trim();

    // Work on the current user-editable string instead of overwriting everything
    let opts = (mysqldumpField.value || '').trim();

    // Ensure a base when empty so users see something sensible
    if (opts.length === 0) {
        opts = '--quick --lock-tables=false --single-transaction --no-autocommit --net_buffer_length=16k --skip-triggers --skip-routines --skip-events --default-character-set=utf8mb4 --compact --extended-insert --compress --opt --hex-blob --disable-keys';
    }

    // Helper: remove a flag (no value)
    const removeFlag = (s, flag) => s.replace(new RegExp('(?:^|\\s)'+flag.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')+'(?=\\s|$)', 'g'), '').replace(/\s{2,}/g, ' ').trim();
    // Helper: ensure a flag is present (no value)
    const ensureFlag = (s, flag) => (new RegExp('(?:^|\\s)'+flag.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')+'(?=\\s|$)').test(s) ? s : (s + ' ' + flag)).trim();
    // Helper: set a key=value style flag, replacing previous occurrences
    const setKeyValueFlag = (s, key, value) => {
        // Remove existing key occurrences
        s = s.replace(new RegExp('(?:^|\\s)'+key+'=\\S+(?=\\s|$)', 'g'), '').replace(/\s{2,}/g, ' ').trim();
        // Add only if value provided
        if (value) {
            s = (s + ' ' + key + '=' + value).trim();
        }
        return s;
    };

    // Toggle table structure flags
    if (includeTableInfo) {
        // Remove the flags when including structure
        opts = removeFlag(opts, '--no-create-db');
        opts = removeFlag(opts, '--no-create-info');
    } else {
        // Ensure both flags when excluding structure
        opts = ensureFlag(opts, '--no-create-db');
        opts = ensureFlag(opts, '--no-create-info');
    }

    // Handle max_allowed_packet - always keep it, just update the value
    const normalizedMem = memoryValue.toUpperCase() || '256M';
    opts = setKeyValueFlag(opts, '--max_allowed_packet', normalizedMem);

    // Normalize spaces
    opts = opts.replace(/\s{2,}/g, ' ').trim();

    // Update the field without breaking user edits elsewhere
    mysqldumpField.value = opts;

    // Show visual feedback
    if (autoUpdateIndicator) {
        autoUpdateIndicator.textContent = '✓ Auto-updated based on current settings';
        autoUpdateIndicator.style.color = '#28a745';
        setTimeout(() => {
            autoUpdateIndicator.textContent = 'Auto-updating based on current settings...';
            autoUpdateIndicator.style.color = '#007bff';
        }, 1200);
    }
}

function testConnection() {
    const btn = document.getElementById('testConnectionBtn');
    const icon = btn.querySelector('.test-icon');
    const text = btn.querySelector('.test-text');
    const section = btn.closest('.settings-section');
    
    // Store original values
    const originalIcon = icon.textContent;
    const originalText = text.textContent;
    
    // Set loading state
    btn.disabled = true;
    btn.classList.add('loading');
    icon.textContent = '⏳';
    text.textContent = 'Testing...';
    
    // Remove any previous success/error states
    section.classList.remove('success', 'error');
    btn.classList.remove('success', 'error');

    return fetch('/api/test-connection', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            // Success state
            btn.classList.add('success');
            icon.textContent = '✅';
            text.textContent = 'Connected';
            section.classList.add('success');
            showToast('MySQL connection test successful!', 'success');
            return true;
        } else {
            // Error state
            btn.classList.add('error');
            section.classList.add('error');
            icon.textContent = '❌';
            text.textContent = 'Failed';
            showToast('MySQL connection test failed: ' + data.error, 'error');
            return false;
        }
    })
    .catch(error => {
        console.error('Error testing MySQL connection:', error);
        btn.classList.add('error');
        section.classList.add('error');
        icon.textContent = '❌';
        text.textContent = 'Error';
        showToast('Error testing MySQL connection', 'error');
        return false;
    })
    .finally(() => {
        btn.disabled = false;
        btn.classList.remove('loading');
        
        // Keep success/error state permanently, only reset icon and text after 3 seconds
        setTimeout(() => {
            icon.textContent = originalIcon;
            text.textContent = originalText;
        }, 3000);
    });
}

function detectBinaryPaths() {
    const btn = document.getElementById('detectBinaryBtn');
    const icon = btn.querySelector('.test-icon');
    const text = btn.querySelector('.test-text');
    const section = btn.closest('.settings-section');
    
    // Store original values
    const originalIcon = icon.textContent;
    const originalText = text.textContent;
    
    // Set loading state
    btn.disabled = true;
    btn.classList.add('loading');
    icon.textContent = '⏳';
    text.textContent = 'Detecting...';
    
    // Remove any previous success/error states
    section.classList.remove('success', 'error');
    btn.classList.remove('success', 'error');

    fetch('/api/detect-binary', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            // Success state
            btn.classList.add('success');
            icon.textContent = '✅';
            text.textContent = 'Detected';
            section.classList.add('success');
            
            // Update form fields with detected paths
            if (data.detected) {
                if (data.detected.mysqldump) {
                    const dumpField = document.getElementById('binary_dump');
                    if (dumpField) dumpField.value = data.detected.mysqldump;
                }
                if (data.detected.mysqlcheck) {
                    const checkField = document.getElementById('binary_check');
                    if (checkField) checkField.value = data.detected.mysqlcheck;
                }
                if (data.detected.mysqlbinlog) {
                    const binlogField = document.getElementById('binary_binlog');
                    if (binlogField) binlogField.value = data.detected.mysqlbinlog;
                }
            }
            
            showToast('Binary paths detected successfully!', 'success');
        } else {
            // Error state
            btn.classList.add('error');
            section.classList.add('error');
            icon.textContent = '❌';
            text.textContent = 'Failed';
            showToast('Binary detection failed: ' + (data.error || data.message || 'Unknown error'), 'error');
        }
    })
    .catch(error => {
        console.error('Error detecting binary paths:', error);
        btn.classList.add('error');
        section.classList.add('error');
        icon.textContent = '❌';
        text.textContent = 'Error';
        showToast('Error detecting binary paths', 'error');
    })
    .finally(() => {
        btn.disabled = false;
        btn.classList.remove('loading');
        
        // Keep success/error state permanently, only reset icon and text after 3 seconds
        setTimeout(() => {
            icon.textContent = originalIcon;
            text.textContent = originalText;
        }, 3000);
    });
}

function validateBinaryConfiguration() {
    const btn = document.getElementById('validateBinaryBtn');
    const icon = btn.querySelector('.test-icon');
    const text = btn.querySelector('.test-text');
    const section = btn.closest('.settings-section');
    
    // Store original values
    const originalIcon = icon.textContent;
    const originalText = text.textContent;
    
    // Set loading state
    btn.disabled = true;
    btn.classList.add('loading');
    icon.textContent = '⏳';
    text.textContent = 'Validating...';
    
    // Remove any previous success/error states
    section.classList.remove('success', 'error');
    btn.classList.remove('success', 'error');

    fetch('/api/validate-binary', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' }
    })
    .then(response => response.json())
    .then(data => {
        // ALWAYS update binlog format if provided (regardless of success/failure)
        if (data.binlog_format) {
            const binlogFormatElement = document.getElementById('binlog_format');
            if (binlogFormatElement) {
                binlogFormatElement.value = data.binlog_format;
            }
        }
        
        // ALWAYS update binlog path if provided (regardless of success/failure)
        if (data.binlog_path) {
            const binlogPathElement = document.getElementById('binlog_path');
            if (binlogPathElement) {
                binlogPathElement.value = data.binlog_path;
            }
        }
        
        if (data.success) {
            // Success state
            btn.classList.add('success');
            icon.textContent = '✅';
            text.textContent = 'Valid';
            section.classList.add('success');
            showToast('Binary configuration validation successful!', 'success');
        } else {
            // Error state
            btn.classList.add('error');
            section.classList.add('error');
            icon.textContent = '❌';
            text.textContent = 'Invalid';
            showToast('Binary validation failed: ' + (data.error || data.message || 'Unknown error'), 'error');
        }
    })
    .catch(error => {
        console.error('Error validating binary configuration:', error);
        btn.classList.add('error');
        section.classList.add('error');
        icon.textContent = '❌';
        text.textContent = 'Error';
        showToast('Error validating binary configuration', 'error');
    })
    .finally(() => {
        btn.disabled = false;
        btn.classList.remove('loading');
        
        // Keep success/error state permanently, only reset icon and text after 3 seconds
        setTimeout(() => {
            icon.textContent = originalIcon;
            text.textContent = originalText;
        }, 3000);
    });
}

// Setup event listeners when DOM is ready
document.addEventListener('DOMContentLoaded', function() {
    // Binary detection button
    const detectBtn = document.getElementById('detectBinaryBtn');
    if (detectBtn) {
        detectBtn.addEventListener('click', function() {
            detectBinaryPaths();
        });
    }
});


