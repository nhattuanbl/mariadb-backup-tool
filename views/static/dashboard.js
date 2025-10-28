// MariaDB Backup Tool - Dashboard Management

function checkLoggingStatus() {
    fetch('/api/logging/status')
        .then(response => response.json())
        .then(data => {
            if (!data.success) {
                showToast('Logging Error: ' + data.error, 'error');
                console.error('Logging system error:', data.error);
            }
        })
        .catch(error => {
            console.error('Error checking logging status:', error);
            showToast('Failed to check logging system status', 'error');
        });
}

function loadDashboardData() {
    // Load dashboard metrics
    loadDashboardMetrics();
    
    // Load recent activity (page 1 by default)
    loadRecentActivity(1);
}

function loadDashboardMetrics() {
    // Load database count
    loadDatabaseCount();
    
    // Load backup schedule
    loadBackupSchedule();
    
    // Load scheduler status
    loadSchedulerStatus();
    
    // Load machine disk usage
    loadMachineDiskUsage();
}

function loadDatabaseCount() {
    // Get database count from the latest backup summary
    fetch('/api/backup/running')
        .then(response => response.json())
        .then(data => {
            if (data.success && data.data && data.data.summaries && data.data.summaries.length > 0) {
                const latestSummary = data.data.summaries[0];
                const totalDatabases = latestSummary.total_db_count || 0;
                
                document.getElementById('total-databases').textContent = totalDatabases;
                
                // Update trend (compare with previous summary if available)
                if (data.data.summaries.length > 1) {
                    const previousSummary = data.data.summaries[1];
                    const previousCount = previousSummary.total_db_count || 0;
                    const trend = totalDatabases - previousCount;
                    
                    const trendElement = document.getElementById('db-trend');
                    if (trendElement) {
                        const trendIndicator = trendElement.querySelector('.trend-indicator');
                        const trendLabel = trendElement.querySelector('.trend-label');
                        
                        if (trendIndicator) {
                            trendIndicator.textContent = trend >= 0 ? `+${trend}` : `${trend}`;
                            trendIndicator.className = `trend-indicator ${trend >= 0 ? 'positive' : 'negative'}`;
                        }
                        if (trendLabel) {
                            trendLabel.textContent = 'vs last backup';
                        }
                    }
                } else {
                    // No previous backup to compare with
                    const trendElement = document.getElementById('db-trend');
                    if (trendElement) {
                        const trendIndicator = trendElement.querySelector('.trend-indicator');
                        const trendLabel = trendElement.querySelector('.trend-label');
                        
                        if (trendIndicator) {
                            trendIndicator.textContent = '+0';
                            trendIndicator.className = 'trend-indicator positive';
                        }
                        if (trendLabel) {
                            trendLabel.textContent = 'vs last period';
                        }
                    }
                }
            } else {
                document.getElementById('total-databases').textContent = '0';
                // Set default trend when no data
                const trendElement = document.getElementById('db-trend');
                if (trendElement) {
                    const trendIndicator = trendElement.querySelector('.trend-indicator');
                    const trendLabel = trendElement.querySelector('.trend-label');
                    
                    if (trendIndicator) {
                        trendIndicator.textContent = '+0';
                        trendIndicator.className = 'trend-indicator positive';
                    }
                    if (trendLabel) {
                        trendLabel.textContent = 'vs last period';
                    }
                }
            }
        })
        .catch(error => {
            console.error('Error loading database count:', error);
            document.getElementById('total-databases').textContent = '0';
            // Set default trend on error
            const trendElement = document.getElementById('db-trend');
            if (trendElement) {
                const trendIndicator = trendElement.querySelector('.trend-indicator');
                const trendLabel = trendElement.querySelector('.trend-label');
                
                if (trendIndicator) {
                    trendIndicator.textContent = '+0';
                    trendIndicator.className = 'trend-indicator positive';
                }
                if (trendLabel) {
                    trendLabel.textContent = 'vs last period';
                }
            }
        });
}

function loadBackupSchedule() {
    // Load schedule information from the dedicated API endpoint
    fetch('/api/schedule/info')
        .then(response => response.json())
        .then(data => {
            if (data.success && data.schedule) {
                const schedule = data.schedule;
                const intervalHours = schedule.interval_hours || 24;
                const startTime = schedule.start_time || "09:00";
                const defaultMode = schedule.default_mode || "auto";
                const lastBackupTime = schedule.last_backup_time;
                const nextBackupTime = schedule.next_backup_time;
                const isDisabled = schedule.is_disabled || false;
                
                // Update schedule mode display
                document.getElementById('schedule-mode').textContent = defaultMode.charAt(0).toUpperCase() + defaultMode.slice(1);
                
                // Update last run time
                if (lastBackupTime) {
                    const lastRunTime = formatTime(lastBackupTime);
                    document.getElementById('last-run-time').textContent = lastRunTime;
                } else {
                    document.getElementById('last-run-time').textContent = 'Never';
                }
                
                // Update next run time - handle disabled scheduler
                if (isDisabled) {
                    document.getElementById('next-run-time').textContent = 'Disabled';
                } else if (nextBackupTime && nextBackupTime !== 'Disabled') {
                    const nextRunTime = formatNextRunTime(nextBackupTime);
                    document.getElementById('next-run-time').textContent = nextRunTime;
                } else {
                    document.getElementById('next-run-time').textContent = 'Error calculating';
                }
            } else {
                // Set default values on error
                document.getElementById('schedule-mode').textContent = 'Auto';
                document.getElementById('next-run-time').textContent = 'Error loading schedule';
                document.getElementById('last-run-time').textContent = 'Error loading data';
            }
        })
        .catch(error => {
            console.error('Error loading backup schedule:', error);
            // Set default values on error
            document.getElementById('schedule-mode').textContent = 'Auto';
            document.getElementById('next-run-time').textContent = 'Error loading schedule';
            document.getElementById('last-run-time').textContent = 'Error loading data';
        });
}

function loadSchedulerStatus() {
    fetch('/api/schedule/status')
        .then(response => response.json())
        .then(data => {
            if (data.success && data.status) {
                const status = data.status;
                const indicator = document.getElementById('scheduler-indicator');
                const statusText = document.getElementById('scheduler-status-text');
                
                if (indicator && statusText) {
                    // Check if scheduler is disabled (interval = 0)
                    if (status.config && status.config.interval_hours === 0) {
                        indicator.className = 'status-indicator stopped';
                        statusText.textContent = 'Disabled';
                    } else if (status.running) {
                        indicator.className = 'status-indicator running';
                        statusText.textContent = 'Running';
                    } else if (status.error) {
                        indicator.className = 'status-indicator error';
                        statusText.textContent = 'Error';
                    } else {
                        indicator.className = 'status-indicator stopped';
                        statusText.textContent = 'Stopped';
                    }
                }
            } else {
                // Set error state
                const indicator = document.getElementById('scheduler-indicator');
                const statusText = document.getElementById('scheduler-status-text');
                
                if (indicator && statusText) {
                    indicator.className = 'status-indicator error';
                    statusText.textContent = 'Error';
                }
            }
        })
        .catch(error => {
            console.error('Error loading scheduler status:', error);
            // Set error state
            const indicator = document.getElementById('scheduler-indicator');
            const statusText = document.getElementById('scheduler-status-text');
            
            if (indicator && statusText) {
                indicator.className = 'status-indicator error';
                statusText.textContent = 'Error';
            }
        });
}

function formatNextRunTime(nextBackupTime) {
    try {
        const nextRun = new Date(nextBackupTime);
        const now = new Date();
        const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
        const tomorrow = new Date(today.getTime() + 24 * 60 * 60 * 1000);
        
        const hours = nextRun.getHours().toString().padStart(2, '0');
        const minutes = nextRun.getMinutes().toString().padStart(2, '0');
        const day = nextRun.getDate().toString().padStart(2, '0');
        const month = (nextRun.getMonth() + 1).toString().padStart(2, '0');
        
        // Check if it's today or tomorrow
        if (nextRun.toDateString() === today.toDateString()) {
            return `Today ${hours}:${minutes}`;
        } else if (nextRun.toDateString() === tomorrow.toDateString()) {
            return `Tomorrow ${hours}:${minutes}`;
        } else {
            return `${day}/${month} ${hours}:${minutes}`;
        }
    } catch (error) {
        console.error('Error formatting next run time:', error);
        return 'Error formatting';
    }
}

function formatTime(dateTimeString) {
    try {
        const date = new Date(dateTimeString);
        const now = new Date();
        const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
        const yesterday = new Date(today.getTime() - 24 * 60 * 60 * 1000);
        
        const hours = date.getHours().toString().padStart(2, '0');
        const minutes = date.getMinutes().toString().padStart(2, '0');
        const day = date.getDate().toString().padStart(2, '0');
        const month = (date.getMonth() + 1).toString().padStart(2, '0');
        const year = date.getFullYear();
        
        // Check if it's today, yesterday, or older
        if (date.toDateString() === today.toDateString()) {
            return `Today ${hours}:${minutes}`;
        } else if (date.toDateString() === yesterday.toDateString()) {
            return `Yesterday ${hours}:${minutes}`;
        } else {
            // For older dates, show full date
            return `${day}/${month}/${year} ${hours}:${minutes}`;
        }
    } catch (error) {
        console.error('Error formatting time:', error);
        return 'Invalid date';
    }
}

function startScheduledBackup() {
    const backupBtn = document.getElementById('backup-now-btn');
    if (!backupBtn) return;
    
    // Disable button and show loading state
    backupBtn.disabled = true;
    backupBtn.innerHTML = '<span class="btn-icon">⏳</span>Starting...';
    
    // Get schedule configuration to get the default backup mode
    fetch('/api/schedule/info')
        .then(response => response.json())
        .then(scheduleData => {
            if (!scheduleData.success) {
                throw new Error(scheduleData.error || 'Failed to get schedule configuration');
            }
            
            const defaultMode = scheduleData.schedule.default_mode || 'auto';
            
            // Get all databases
            return fetch('/api/databases')
                .then(response => response.json())
                .then(data => {
                    if (!data.success) {
                        throw new Error(data.error || 'Failed to get databases');
                    }
                    
                    const allDatabases = data.databases || [];
                    
                    // Start backup with the actual default mode and all databases
                    return fetch('/api/backup/start', {
                        method: 'POST',
                        headers: {
                            'Content-Type': 'application/json',
                        },
                        body: JSON.stringify({
                            backup_mode: defaultMode, // Use the actual default backup mode from config
                            databases: allDatabases // Backup all databases
                        })
                    });
                });
        })
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                showToast('Backup started successfully!', 'success');
                // Refresh schedule info after a short delay
                setTimeout(() => {
                    loadBackupSchedule();
                }, 2000);
            } else {
                showToast('Failed to start backup: ' + (data.error || 'Unknown error'), 'error');
            }
        })
        .catch(error => {
            console.error('Error starting backup:', error);
            showToast('Error starting backup: ' + error.message, 'error');
        })
        .finally(() => {
            // Re-enable button
            backupBtn.disabled = false;
            backupBtn.innerHTML = '<span class="btn-icon">🚀</span>Backup Now';
        });
}

function loadMachineDiskUsage() {
    // Get machine disk usage from system metrics
    fetch('/api/system-metrics')
        .then(response => response.json())
        .then(data => {
            if (data.success && data.metrics) {
                const usedGB = Math.round((data.metrics.disk_used || 0) * 100) / 100;
                const totalGB = Math.round((data.metrics.disk_total || 0) * 100) / 100;
                const usagePercent = Math.round(data.metrics.disk_percent || 0);
                
                
                document.getElementById('machine-disk-usage').textContent = `${usedGB} GB`;
                document.getElementById('machine-usage-fill').style.width = `${usagePercent}%`;
                document.getElementById('machine-usage-text').textContent = `${usagePercent}% of ${totalGB} GB total`;
                
                // Apply color class based on usage percentage
                const usageFill = document.getElementById('machine-usage-fill');
                usageFill.className = 'usage-fill'; // Reset classes
                if (usagePercent < 50) {
                    usageFill.classList.add('low-usage');
                } else if (usagePercent >= 50 && usagePercent < 85) {
                    usageFill.classList.add('medium-usage');
                } else if (usagePercent >= 85) {
                    usageFill.classList.add('high-usage');
                }
            } else {
                document.getElementById('machine-disk-usage').textContent = '0 GB';
                document.getElementById('machine-usage-fill').style.width = '0%';
                document.getElementById('machine-usage-text').textContent = '0% of total capacity';
                
                // Apply low usage color for 0% usage
                const usageFill = document.getElementById('machine-usage-fill');
                usageFill.className = 'usage-fill low-usage';
            }
        })
        .catch(error => {
            console.error('Error loading machine disk usage:', error);
            document.getElementById('machine-disk-usage').textContent = '0 GB';
            document.getElementById('machine-usage-fill').style.width = '0%';
            document.getElementById('machine-usage-text').textContent = '0% of total capacity';
            
            // Apply low usage color for 0% usage
            const usageFill = document.getElementById('machine-usage-fill');
            usageFill.className = 'usage-fill low-usage';
        });
}

let recentActivityPagination = {
    currentPage: 1,
    totalPages: 1,
    totalCount: 0,
    limit: 5
};

function loadRecentActivity(page = 1) {
    const recentActivityElement = document.getElementById('recent-activity');
    if (!recentActivityElement) return;
    
    // Show loading state
    recentActivityElement.innerHTML = '<p class="text-muted">Loading recent activity...</p>';
    
    // Hide pagination controls while loading
    const paginationElement = document.getElementById('recent-activity-pagination');
    if (paginationElement) {
        paginationElement.style.display = 'none';
    }
    
    // Fetch recent jobs data with pagination
    fetch(`/api/backup/recent-activity?page=${page}&limit=${recentActivityPagination.limit}`)
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                displayRecentActivity(data.data);
                updatePaginationControls(data.data.pagination);
            } else {
                recentActivityElement.innerHTML = 
                    '<p class="text-error">Failed to load recent activity: ' + data.error + '</p>';
            }
        })
        .catch(error => {
            console.error('Error loading recent activity:', error);
            recentActivityElement.innerHTML = 
                '<p class="text-error">Error loading recent activity</p>';
        });
}

function displayRecentActivity(data) {
    const recentActivityElement = document.getElementById('recent-activity');
    if (!recentActivityElement) return;
    
    const summaries = data && data.summaries ? data.summaries : [];
    
    if (!summaries || summaries.length === 0) {
        recentActivityElement.innerHTML = 
            '<p class="text-muted">No recent backup activity</p>';
        return;
    }
    
    // Sort summaries: running first, then completed by most recent
    const sortedSummaries = summaries.sort((a, b) => {
        // Running summaries first
        if (a.state === 'running' && b.state !== 'running') return -1;
        if (a.state !== 'running' && b.state === 'running') return 1;
        
        // Then sort by most recent
        const timeA = a.state === 'running' ? new Date(a.created_at).getTime() : new Date(a.completed_at).getTime();
        const timeB = b.state === 'running' ? new Date(b.created_at).getTime() : new Date(b.completed_at).getTime();
        return timeB - timeA; // Most recent first
    });
    
    let activityHTML = '';
    sortedSummaries.forEach(summary => { // Remove hardcoded slice(0, 5)
        const isRunning = summary.state === 'running';
        const isCompleted = summary.state === 'completed';
        
        let statusText = '';
        let statusClass = '';
        let timeInfo = '';
        
        if (isRunning) {
            statusText = 'Running';
            statusClass = 'running';
            timeInfo = `Started: ${formatTime(summary.created_at)}`;
        } else if (isCompleted) {
            const successCount = (summary.total_full || 0) + (summary.total_incremental || 0);
            const failedCount = summary.total_failed || 0;
            const totalProcessed = successCount + failedCount;
            const totalCount = summary.total_db_count || 0;
            
            if (failedCount === 0 && totalProcessed === totalCount) {
                // All successful and all databases processed
                statusText = 'Completed';
                statusClass = 'completed-success';
            } else if (successCount === 0 && totalProcessed === totalCount) {
                // All failed and all databases processed
                statusText = 'Completed';
                statusClass = 'completed-failed';
            } else if (totalProcessed < totalCount) {
                // Some databases not processed
                statusText = 'Completed (Partial)';
                statusClass = 'completed-warning';
            } else {
                // Mixed results
                statusText = 'Completed with Error';
                statusClass = 'completed-warning';
            }
            
            // Calculate duration from created_at to completed_at
            if (summary.completed_at) {
                const startTime = new Date(summary.created_at).getTime();
                const endTime = new Date(summary.completed_at).getTime();
                const durationMs = endTime - startTime;
                const duration = formatDurationFromMs(durationMs);
                timeInfo = `Duration: ${duration}`;
            } else {
                timeInfo = `Completed: ${formatTime(summary.created_at)}`;
            }
        } else {
            statusText = 'Unknown';
            statusClass = 'unknown';
            timeInfo = `Created: ${formatTime(summary.created_at)}`;
        }
        
        const successRate = summary.total_db_count > 0 ? 
            Math.round(((summary.total_full || 0) + (summary.total_incremental || 0)) / summary.total_db_count * 100) : 0;
        
        // Calculate total processed for display
        const totalProcessed = (summary.total_full || 0) + (summary.total_incremental || 0) + (summary.total_failed || 0);
        const totalCount = summary.total_db_count || 0;
        const pendingCount = totalCount - totalProcessed;
        
        // Debug logging to help identify issues
        console.log({
            total_db_count: summary.total_db_count,
            total_full: summary.total_full,
            total_incremental: summary.total_incremental,
            total_failed: summary.total_failed,
            totalProcessed,
            pendingCount,
            successRate
        });
        
        // Get icon for backup mode
        const modeIcon = getBackupModeIcon(summary.backup_mode);
        
        // Format job_id timestamp for display
        const formattedTimestamp = formatJobIdTimestamp(summary.job_id);
        
        activityHTML += `
            <div class="activity-item ${statusClass}" onclick="navigateToBackupWithFilter('${summary.job_id}', '${summary.state}')">
                <div class="activity-header">
                    <div class="activity-info">
                        <span class="activity-job-id">#${summary.job_id}</span>
                        <span class="activity-timestamp">${formattedTimestamp}</span>
                        <span class="activity-status">${statusText}</span>
                        <span class="activity-mode-icon" title="${summary.backup_mode}">${modeIcon}</span>
                    </div>
                    <div class="activity-time">${timeInfo}</div>
                </div>
                <div class="activity-summary">
                    <div class="summary-stats">
                        <span class="summary-stat info">📊 ${summary.total_db_count} databases</span>
                        <span class="summary-stat success clickable-stat" onclick="event.stopPropagation(); navigateToBackupWithFilter('${summary.job_id}', 'done')">✅ ${(summary.total_full || 0) + (summary.total_incremental || 0)} success</span>
                        <span class="summary-stat failed clickable-stat" onclick="event.stopPropagation(); navigateToBackupWithFilter('${summary.job_id}', 'failed')">❌ ${summary.total_failed} failed</span>
                        ${pendingCount > 0 ? `<span class="summary-stat warning">⏸️ ${pendingCount} pending</span>` : ''}
                        <span class="summary-stat success">📈 ${successRate}% success rate</span>
                    </div>
                    <div class="summary-size">
                        <span class="size-label">Total Size:</span>
                        <span class="size-value">${formatSizeFromKB(summary.total_size_kb || 0)}</span>
                    </div>
                    <div class="summary-disk-size">
                        <span class="size-label">Disk Size:</span>
                        <span class="size-value">${formatSizeFromKB(summary.total_disk_size || 0)}</span>
                    </div>
                </div>
            </div>
        `;
    });
    
    recentActivityElement.innerHTML = activityHTML;
}

function formatJobIdTimestamp(jobId) {
    try {
        // Convert job_id (timestamp) to Date object
        const timestamp = parseInt(jobId);
        const date = new Date(timestamp * 1000); // Convert Unix timestamp to milliseconds
        
        // Format date and time
        const day = date.toLocaleDateString('en-US', { 
            weekday: 'short', 
            month: 'short', 
            day: 'numeric' 
        });
        const time = date.toLocaleTimeString('en-US', { 
            hour: '2-digit', 
            minute: '2-digit',
            hour12: true 
        });
        
        return `${day} ${time}`;
    } catch (error) {
        // Fallback to original job_id if parsing fails
        console.warn('Failed to parse job_id timestamp:', jobId, error);
        return `#${jobId}`;
    }
}

function updatePaginationControls(pagination) {
    const paginationElement = document.getElementById('recent-activity-pagination');
    if (!paginationElement) return;
    
    // Update pagination state
    recentActivityPagination.currentPage = pagination.current_page;
    recentActivityPagination.totalPages = pagination.total_pages;
    recentActivityPagination.totalCount = pagination.total_count;
    
    // Update pagination info
    const paginationInfo = document.getElementById('pagination-info');
    if (paginationInfo) {
        const startItem = (pagination.current_page - 1) * pagination.limit + 1;
        const endItem = Math.min(pagination.current_page * pagination.limit, pagination.total_count);
        paginationInfo.textContent = `Showing ${startItem}-${endItem} of ${pagination.total_count}`;
    }
    
    // Update pagination buttons
    const prevBtn = document.getElementById('prev-page-btn');
    const nextBtn = document.getElementById('next-page-btn');
    
    if (prevBtn) {
        prevBtn.disabled = !pagination.has_prev;
    }
    
    if (nextBtn) {
        nextBtn.disabled = !pagination.has_next;
    }
    
    // Show pagination controls if there are multiple pages
    if (pagination.total_pages > 1) {
        paginationElement.style.display = 'flex';
    } else {
        paginationElement.style.display = 'none';
    }
}

function goToPreviousPage() {
    if (recentActivityPagination.currentPage > 1) {
        loadRecentActivity(recentActivityPagination.currentPage - 1);
    }
}

function goToNextPage() {
    if (recentActivityPagination.currentPage < recentActivityPagination.totalPages) {
        loadRecentActivity(recentActivityPagination.currentPage + 1);
    }
}

function setupDashboardEventListeners() {
    // Setup pagination button event listeners
    const prevBtn = document.getElementById('prev-page-btn');
    const nextBtn = document.getElementById('next-page-btn');
    
    if (prevBtn) {
        prevBtn.addEventListener('click', goToPreviousPage);
    }
    
    if (nextBtn) {
        nextBtn.addEventListener('click', goToNextPage);
    }
}

function getBackupModeIcon(backupMode) {
    // Check if it's a scheduled backup
    const isScheduled = backupMode.startsWith('scheduled-');
    
    // Extract the base mode
    let baseMode = backupMode;
    if (isScheduled) {
        // Remove 'scheduled-' prefix and get the actual mode
        baseMode = backupMode.replace('scheduled-', '');
        
        // Handle special cases for scheduled-auto-full and scheduled-auto-inc
        if (baseMode === 'auto-full') {
            return '⏰🤖'; // Clock + Robot for scheduled auto full
        } else if (baseMode === 'auto-inc') {
            return '⏰🤖'; // Clock + Robot for scheduled auto incremental
        }
    }
    
    // Icon for scheduled backups
    if (isScheduled) {
        switch(baseMode) {
            case 'full':
                return '⏰🔄'; // Clock + Refresh for scheduled full
            case 'inc':
                return '⏰⚡'; // Clock + Lightning for scheduled incremental
            case 'auto':
                return '⏰🤖'; // Clock + Robot for scheduled auto
            default:
                return '⏰❓'; // Clock + Question mark for unknown scheduled
        }
    }
    
    // Icons for manual backups
    switch(baseMode) {
        case 'auto':
            return '✋🤖'; // Hand + Robot for manual auto mode
        case 'full':
            return '✋🔄'; // Hand + Refresh for manual full backup
        case 'incremental':
            return '✋⚡'; // Hand + Lightning for manual incremental
        default:
            return '❓'; // Question mark for unknown
    }
}

function navigateToBackupWithFilter(jobId, status) {
    // Build URL with query parameters for filtering
    const params = new URLSearchParams();
    params.append('job_id', jobId);
    params.append('status', status);
    
    // Navigate to backup page with filters
    window.location.href = `/backup?${params.toString()}`;
}

function clearBackupHistory() {
    const deleteBtn = document.getElementById('delete-history-btn');
    if (!deleteBtn) return;
    
    // Show confirmation dialog
    if (!confirm('Are you sure you want to delete all backup history? This action cannot be undone.')) {
        return;
    }
    
    // Disable button and show loading state
    deleteBtn.disabled = true;
    deleteBtn.innerHTML = '<span class="btn-icon">⏳</span>Deleting...';
    
    // Call the API to clear backup history
    fetch('/api/backup/history/clear', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        }
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            showToast('Backup history cleared successfully!', 'success');
            // Reload recent activity to show empty state
            loadRecentActivity();
        } else {
            showToast('Failed to clear backup history: ' + (data.error || 'Unknown error'), 'error');
        }
    })
    .catch(error => {
        console.error('Error clearing backup history:', error);
        showToast('Error clearing backup history: ' + error.message, 'error');
    })
    .finally(() => {
        // Re-enable button
        deleteBtn.disabled = false;
        deleteBtn.innerHTML = '<span class="btn-icon">🗑️</span>Delete History';
    });
}

// Initialize dashboard when DOM is loaded
function initDashboard() {
    checkLoggingStatus();
    loadDashboardData();
    setupDashboardEventListeners();
}

function updateTimelineSummary(summary) {
    if (!summary) return;
    
    // Update summary metrics
    const totalSizeElement = document.getElementById('total-backup-size');
    const avgSizeElement = document.getElementById('avg-backup-size');
    const growthRateElement = document.getElementById('growth-rate');
    
    if (totalSizeElement) {
        totalSizeElement.textContent = summary.total_backup_size_gb.toFixed(1) + ' GB';
    }
    
    if (avgSizeElement) {
        avgSizeElement.textContent = summary.average_size_gb.toFixed(1) + ' GB';
    }
    
    if (growthRateElement) {
        const growthRate = summary.growth_rate_percent || 0;
        const sign = growthRate >= 0 ? '+' : '';
        growthRateElement.textContent = sign + growthRate.toFixed(1) + '%';
        growthRateElement.className = `metric-value ${growthRate >= 0 ? 'positive' : 'negative'}`;
    }
}

// Initialize dashboard when DOM is loaded


