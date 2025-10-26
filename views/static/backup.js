// MariaDB Backup Tool - Backup Management

window.currentJobId = null;

window.startBackupAll = function() {
    // Check if buttons are enabled before proceeding
    const startBtn = document.getElementById('startBackupBtn');
    if (startBtn.disabled) {
        showToast('Please ensure database connection and binary validation are successful first.', 'warning');
        return;
    }
    
    // Store original button state
    const originalText = startBtn.textContent;
    const originalDisabled = startBtn.disabled;
    
    // Disable button and show loading state
    startBtn.disabled = true;
    startBtn.textContent = '🔄 Loading Databases...';
    startBtn.classList.add('loading');
    
    // Get all available databases from the system (excluding ignored ones)
    fetch('/api/databases')
        .then(response => response.json())
        .then(data => {
            if (data.success && data.databases && data.databases.length > 0) {
                const allDatabases = data.databases;
                
                if (allDatabases.length === 0) {
                    showToast('No databases available for backup.', 'warning');
                    // Re-enable button
                    startBtn.disabled = originalDisabled;
                    startBtn.textContent = originalText;
                    startBtn.classList.remove('loading');
                    return;
                }
                
                // Update button text to show starting backup
                startBtn.textContent = '🚀 Starting Backup...';
                
                const backupData = {
                    backup_mode: document.getElementById('backup_mode').value,
                    databases: allDatabases
                };
                
                fetch('/api/backup/start', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(backupData)
                })
                .then(response => response.json())
                .then(data => {
                    if (data.success) {
                        window.currentJobId = data.job_id;
                        showToast(`Backup started for all ${allDatabases.length} databases!`, 'success');
                        // Jobs will be updated via WebSocket
                    } else {
                        showToast('Failed to start backup: ' + data.error, 'error');
                    }
                })
                .catch(error => {
                    console.error('Error starting backup:', error);
                    showToast('Error starting backup', 'error');
                })
                .finally(() => {
                    // Re-enable button after backup start attempt
                    startBtn.disabled = originalDisabled;
                    startBtn.textContent = originalText;
                    startBtn.classList.remove('loading');
                });
            } else {
                showToast('Failed to load databases. Please try again.', 'error');
                // Re-enable button
                startBtn.disabled = originalDisabled;
                startBtn.textContent = originalText;
                startBtn.classList.remove('loading');
            }
        })
        .catch(error => {
            console.error('Error loading databases:', error);
            showToast('Error loading databases', 'error');
            // Re-enable button
            startBtn.disabled = originalDisabled;
            startBtn.textContent = originalText;
            startBtn.classList.remove('loading');
        });
};

function handleURLParameters() {
    const urlParams = new URLSearchParams(window.location.search);
    const jobId = urlParams.get('job_id');
    const status = urlParams.get('status');
    
    if (jobId) {
        const jobFilter = document.getElementById('history-job-filter');
        if (jobFilter) {
            jobFilter.value = jobId;
        }
        // Set the global currentJobId for history.js
        window.currentJobId = jobId;
    }
    
    if (status) {
        const statusFilter = document.getElementById('history-status-filter');
        if (statusFilter) {
            statusFilter.value = status;
        }
    }
    
    // Store the URL parameters for later use when history system is initialized
    if (jobId || status) {
        window.urlParameters = { jobId, status };
    }
}

function loadAvailableDatabases() {
    const btn = document.getElementById('loadDatabasesBtn');
    const originalText = btn.textContent;
    
    // Check if buttons are enabled before proceeding
    if (btn.disabled) {
        showToast('Please ensure database connection and binary validation are successful first.', 'warning');
        return;
    }
    
    btn.disabled = true;
    btn.textContent = 'Loading...';

    fetch('/api/databases')
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            displayDatabases(data.databases);
            showToast(`Loaded ${data.databases.length} databases successfully!`, 'success');
        } else {
            showToast('Failed to load databases: ' + data.error, 'error');
            document.getElementById('database-list').innerHTML =
                '<p class="text-error">Failed to load databases. Please check your MySQL connection.</p>';
        }
    })
    .catch(error => {
        console.error('Error loading databases:', error);
        showToast('Error loading databases', 'error');
    })
    .finally(() => {
        btn.disabled = false;
        btn.textContent = originalText;
    });
}

function displayDatabases(databases) {
    const container = document.getElementById('database-list');
    
    if (databases.length === 0) {
        container.innerHTML = '<p class="text-muted">No databases found</p>';
        updateDatabaseCount(0, 0);
        return;
    }

    const html = databases.map(db => `
        <div class="database-item">
            <input type="checkbox" name="databases" value="${db}" id="db_${db}" onchange="updateBackupSelectedButton()">
            <label for="db_${db}">${db}</label>
        </div>
    `).join('');

    container.innerHTML = html;
    
    // Show search box when databases are loaded
    const searchBox = document.getElementById('databaseSearchBox');
    if (searchBox) {
        searchBox.style.display = 'flex';
    }
    
    // Setup search functionality
    setupDatabaseSearch();
    
    // Initially disable backup selected button (no databases selected yet)
    const backupSelectedBtn = document.getElementById('backupSelectedBtn');
    if (backupSelectedBtn) {
        backupSelectedBtn.disabled = true;
    }
    
    // Update database count display
    updateDatabaseCount(0, databases.length);
    
    // Update button state based on current selection
    updateBackupSelectedButton();
}

// Update database count display
function updateDatabaseCount(selected, total) {
    const countElement = document.getElementById('database-count');
    if (countElement) {
        if (total === 0) {
            countElement.textContent = '';
            countElement.className = 'database-count';
        } else {
            countElement.textContent = `${selected}/${total}`;
            countElement.className = 'database-count loaded';
        }
    }
}

function selectAllDatabases() {
    const checkboxes = document.querySelectorAll('input[name="databases"]');
    checkboxes.forEach(cb => cb.checked = true);
    updateBackupSelectedButton();
}

function selectNoneDatabases() {
    const checkboxes = document.querySelectorAll('input[name="databases"]');
    checkboxes.forEach(cb => cb.checked = false);
    updateBackupSelectedButton();
}

function updateBackupSelectedButton() {
    const checkboxes = document.querySelectorAll('input[name="databases"]:checked');
    const allCheckboxes = document.querySelectorAll('input[name="databases"]');
    const backupSelectedBtn = document.getElementById('backupSelectedBtn');
    
    if (backupSelectedBtn) {
        const hasSelection = checkboxes.length > 0;
        backupSelectedBtn.disabled = !hasSelection;
        
        // Update button text to show selection count
        if (hasSelection) {
            backupSelectedBtn.textContent = `⚡ Backup Selected (${checkboxes.length})`;
        } else {
            backupSelectedBtn.textContent = '⚡ Backup Selected';
        }
    }
    
    // Update database count display
    updateDatabaseCount(checkboxes.length, allCheckboxes.length);
}

function setupDatabaseSearch() {
    const searchInput = document.getElementById('databaseSearch');
    const databaseList = document.getElementById('database-list');
    
    if (!searchInput || !databaseList) return;
    
    searchInput.addEventListener('input', function() {
        const searchTerm = this.value.toLowerCase();
        const databaseItems = databaseList.querySelectorAll('.database-item');
        
        databaseItems.forEach(item => {
            const label = item.querySelector('label');
            const dbName = label.textContent.toLowerCase();
            
            if (dbName.includes(searchTerm)) {
                item.style.display = 'flex';
            } else {
                item.style.display = 'none';
            }
        });
    });
}

// Backup operations
function startBackupSelected() {
    // Check if buttons are enabled before proceeding
    const backupBtn = document.getElementById('backupSelectedBtn');
    if (backupBtn.disabled) {
        showToast('Please ensure database connection and binary validation are successful first.', 'warning');
        return;
    }
    
    const selectedDatabases = Array.from(document.querySelectorAll('input[name="databases"]:checked'))
        .map(cb => cb.value);
    
    if (selectedDatabases.length === 0) {
        showToast('Please select at least one database to backup', 'warning');
        return;
    }
    
    // Store original button state
    const originalText = backupBtn.textContent;
    const originalDisabled = backupBtn.disabled;
    
    // Disable button and show loading state
    backupBtn.disabled = true;
    backupBtn.textContent = '🚀 Starting Backup...';
    backupBtn.classList.add('loading');
    
    const backupData = {
        backup_mode: document.getElementById('backup_mode').value,
        databases: selectedDatabases
    };
    
    fetch('/api/backup/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(backupData)
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            showToast('Backup job started successfully!', 'success');
            // Jobs will be updated via WebSocket
        } else {
            showToast('Failed to start backup: ' + data.error, 'error');
        }
    })
    .catch(error => {
        console.error('Error starting backup:', error);
        showToast('Error starting backup', 'error');
    })
    .finally(() => {
        // Re-enable button after backup start attempt
        backupBtn.disabled = originalDisabled;
        backupBtn.textContent = originalText;
        backupBtn.classList.remove('loading');
    });
}

function stopAllBackups() {
    const stopBtn = document.getElementById('stopBackupBtn');
    if (stopBtn.disabled) {
        showToast('No running backups to stop.', 'warning');
        return;
    }

    // Confirm before stopping
    if (!confirm('Are you sure you want to stop all running backups? This action cannot be undone.')) {
        return;
    }

    const originalText = stopBtn.textContent;
    stopBtn.disabled = true;
    stopBtn.textContent = 'Stopping...';

    fetch('/api/backup/stop', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({})
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            showToast(data.message || 'All running backups stopped successfully!', 'success');
        } else {
            showToast('Failed to stop backups: ' + (data.error || 'Unknown error'), 'error');
        }
    })
    .catch(error => {
        console.error('Error stopping backups:', error);
        showToast('Error stopping backups', 'error');
    })
    .finally(() => {
        stopBtn.disabled = false;
        stopBtn.textContent = originalText;
    });
}

function startQuickBackup(mode) {
    const backupData = {
        backup_mode: mode,
        databases: [] // Empty array means backup all databases
    };
    
    // Note: This function doesn't have a specific button to manage state
    // It's typically called from other UI elements that should handle their own state
    
    fetch('/api/backup/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(backupData)
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            showToast(`${mode.charAt(0).toUpperCase() + mode.slice(1)} backup started successfully!`, 'success');
        } else {
            showToast('Failed to start backup: ' + data.error, 'error');
        }
    })
    .catch(error => {
        console.error('Error starting backup:', error);
        showToast('Error starting backup', 'error');
    });
}

window.jobGroupStates = new Map();

function displayRunningJobs(data) {
    const container = document.getElementById('running-jobs');
    if (!container) return;
    
    
    // Clear the entire container first
    container.innerHTML = '';
    
    // Handle both old format (array) and new format (object with summaries and jobs)
    let jobs = [];
    let summaries = [];
    let totalRunning = 0;
    let totalJobs = 0;
    let completedJobs = 0;

    if (!data) {
        // Handle null/undefined data
        jobs = [];
        summaries = [];
        totalRunning = 0;
        totalJobs = 0;
        completedJobs = 0;
    } else if (Array.isArray(data)) {
        // Old format - just jobs array
        jobs = data || [];
        totalJobs = jobs.length;
        completedJobs = jobs.filter(job => job.status === 'done').length;
    } else {
        // New format - object with summaries and jobs
        // Handle null values from server
        jobs = data.jobs || [];
        summaries = data.summaries || [];
        totalRunning = data.total_running || 0;
        totalJobs = data.total_jobs || 0;
        completedJobs = data.completed_jobs || 0;
        
        // Convert null to empty array if needed
        if (jobs === null) jobs = [];
        if (summaries === null) summaries = [];
    }

    if (!jobs || jobs.length === 0) {
        container.innerHTML = `
            <div class="cod-loading-container">
                <div class="cod-spinner-wrapper">
                    <div class="cod-outer-ring"></div>
                    <div class="cod-spinner"></div>
                    <div class="cod-spinner-inner"></div>
                    <div class="cod-center-dot"></div>
                    <div class="cod-corner-lines">
                        <div class="cod-corner-line"></div>
                        <div class="cod-corner-line"></div>
                        <div class="cod-corner-line"></div>
                        <div class="cod-corner-line"></div>
                    </div>
                </div>
                <div class="cod-loading-text">Loading</div>
                <div class="cod-loading-dots">...</div>
                <div class="cod-status-bar">
                    <div class="cod-status-bar-fill"></div>
                </div>
                <div class="cod-percentage">0%</div>
            </div>
        `;
        
        // Start the animated elements
        startCODSpinnerAnimation();
        return;
    }

    // Sort jobs: running and optimizing first, then recent completed tasks
    const sortedJobs = jobs.sort((a, b) => {
        // First priority: running and optimizing jobs
        const aIsActive = a.status === 'running' || a.status === 'optimizing';
        const bIsActive = b.status === 'running' || b.status === 'optimizing';
        
        if (aIsActive && !bIsActive) return -1;
        if (!aIsActive && bIsActive) return 1;
        
        // If both are active, prioritize running over optimizing
        if (aIsActive && bIsActive) {
            if (a.status === 'running' && b.status === 'optimizing') return -1;
            if (a.status === 'optimizing' && b.status === 'running') return 1;
        }
        
        // For non-active tasks, sort by completion time (most recent first)
        if (!aIsActive && !bIsActive) {
            const aTime = new Date(a.completed_at || a.started_at).getTime();
            const bTime = new Date(b.completed_at || b.started_at).getTime();
            return bTime - aTime; // Most recent first
        }
        
        return 0;
    });

    // Render jobs grouped by job_id
    const jobsByJobId = {};
    sortedJobs.forEach(job => {
        if (!jobsByJobId[job.job_id]) {
            jobsByJobId[job.job_id] = [];
        }
        jobsByJobId[job.job_id].push(job);
    });

    // Sort job groups: running/optimizing jobs first, then completed jobs by most recent
    const sortedJobIds = Object.keys(jobsByJobId).sort((a, b) => {
        const jobA = jobsByJobId[a];
        const jobB = jobsByJobId[b];
        
        // Check if any job in group A is running or optimizing
        const hasActiveA = jobA.some(job => job.status === 'running' || job.status === 'optimizing');
        const hasActiveB = jobB.some(job => job.status === 'running' || job.status === 'optimizing');
        
        // Active jobs first
        if (hasActiveA && !hasActiveB) return -1;
        if (!hasActiveA && hasActiveB) return 1;
        
        // If both active or both not active, sort by most recent
        const latestA = Math.max(...jobA.map(job => new Date(job.started_at).getTime()));
        const latestB = Math.max(...jobB.map(job => new Date(job.started_at).getTime()));
        return latestB - latestA; // Most recent first
    });

    // Limit the number of job groups shown initially
    const maxInitialGroups = 6;
    const showMoreButton = sortedJobIds.length > maxInitialGroups;
    const groupsToShow = showMoreButton ? sortedJobIds.slice(0, maxInitialGroups) : sortedJobIds;
    
    let jobsHTML = '';
    groupsToShow.forEach((jobId, index) => {
        const jobGroup = jobsByJobId[jobId];
        const firstJob = jobGroup[0];
        
        // Find summary for this job
        const summary = summaries.find(s => s.job_id === jobId);
        
        // Calculate group progress
        const totalProgress = jobGroup.reduce((sum, job) => sum + (job.progress || 0), 0);
        const avgProgress = Math.round(totalProgress / jobGroup.length);
        
        // Calculate group status
        const hasRunning = jobGroup.some(job => job.status === 'running');
        const hasOptimizing = jobGroup.some(job => job.status === 'optimizing');
        const hasFailed = jobGroup.some(job => job.status === 'failed');
        const allDone = jobGroup.every(job => job.status === 'done');
        
        let groupStatus = 'running';
        if (allDone) groupStatus = 'done';
        else if (hasFailed && !hasRunning && !hasOptimizing) groupStatus = 'failed';
        else if (hasRunning || hasOptimizing) groupStatus = 'running';
        
        const statusIcon = getStatusIcon(groupStatus);
        const progressBar = createProgressBar(avgProgress);
        
        // Format time info
        const timeInfo = (hasRunning || hasOptimizing) ? 
            `Started: ${formatTime(firstJob.started_at)}` : 
            `${groupStatus === 'done' ? 'Completed' : 'Failed'}: ${formatTime(firstJob.completed_at || firstJob.started_at)}`;

        // Job group header
        const runningCount = jobGroup.filter(j => j.status === 'running').length;
        const optimizingCount = jobGroup.filter(j => j.status === 'optimizing').length;
        
        // Use backup summary data for success and failed counts instead of counting individual job statuses
        let successCount = 0;
        let failedCount = 0;
        if (summary) {
            successCount = (summary.total_full || 0) + (summary.total_incremental || 0);
            failedCount = summary.total_failed || 0;
            console.log(`Job ${jobId}: Using summary data - success: ${successCount}, failed: ${failedCount}`, summary);
        } else {
            // Fallback to counting individual job statuses if no summary available
            successCount = jobGroup.filter(j => j.status === 'done').length;
            failedCount = jobGroup.filter(j => j.status === 'failed').length;
            console.log(`Job ${jobId}: Using fallback data - success: ${successCount}, failed: ${failedCount}`);
        }
        
        // Check if this job group state is already stored, otherwise use default
        let isExpanded;
        if (window.jobGroupStates.has(jobId)) {
            isExpanded = window.jobGroupStates.get(jobId).expanded;
        } else {
            // First group is expanded by default, others are collapsed
            isExpanded = index === 0;
            // Store the initial state
            window.jobGroupStates.set(jobId, { expanded: isExpanded, scrollPosition: 0 });
        }
        const collapseClass = isExpanded ? 'expanded' : 'collapsed';
        
        jobsHTML += `
            <div class="job-group ${groupStatus}">
                <div class="job-group-header" onclick="toggleJobGroup('${jobId}')">
                    <div class="job-group-info">
                        <span class="job-id">#${jobId}</span>
                        ${runningCount > 0 ? `<span class="job-running">🔄 ${runningCount} running</span>` : ''}
                        ${optimizingCount > 0 ? `<span class="job-optimizing">🔧 ${optimizingCount} optimizing</span>` : ''}
                        ${successCount > 0 ? `<span class="job-success">✅ ${successCount} success</span>` : ''}
                        ${failedCount > 0 ? `<span class="job-failed">❌ ${failedCount} failed</span>` : ''}
                    </div>
                    <div class="job-group-controls">
                        <div class="job-group-time">
                            <span class="job-started">${timeInfo}</span>
                        </div>
                        <button class="collapse-btn ${collapseClass}" onclick="event.stopPropagation(); toggleJobGroup('${jobId}')">
                            <span class="collapse-icon">${isExpanded ? '▼' : '▶'}</span>
                        </button>
                    </div>
                </div>
                <div class="job-group-details ${collapseClass}" id="details-${jobId}">
        `;

        // Sort individual jobs within group: running and optimizing first, then recent completed tasks
        const sortedJobs = jobGroup.sort((a, b) => {
            // Running and optimizing tasks first
            const aIsActive = a.status === 'running' || a.status === 'optimizing';
            const bIsActive = b.status === 'running' || b.status === 'optimizing';
            
            if (aIsActive && !bIsActive) return -1;
            if (!aIsActive && bIsActive) return 1;
            
            // If both are active, prioritize running over optimizing
            if (aIsActive && bIsActive) {
                if (a.status === 'running' && b.status === 'optimizing') return -1;
                if (a.status === 'optimizing' && b.status === 'running') return 1;
            }
            
            // For non-active tasks, sort by completion time (most recent first)
            if (!aIsActive && !bIsActive) {
                const aTime = new Date(a.completed_at || a.started_at).getTime();
                const bTime = new Date(b.completed_at || b.started_at).getTime();
                return bTime - aTime; // Most recent first
            }
            
            return 0;
        });

        // Individual jobs in this group
        sortedJobs.forEach(job => {
            const icon = job.backup_type.includes('full') ? '🔄' : '⚡';
            const jobStatusIcon = getStatusIcon(job.status);
            const jobProgressBar = createProgressBar(job.progress);
            const size = formatSizeFromKB(job.estimated_size_kb || job.actual_size_kb || 0);
            
            // Calculate duration for completed tasks
            let jobTimeInfo;
            if (job.status === 'done' || job.status === 'completed') {
                if (job.started_at && job.completed_at) {
                    const startTime = new Date(job.started_at).getTime();
                    const endTime = new Date(job.completed_at).getTime();
                    
                    // Check if dates are valid
                    if (!isNaN(startTime) && !isNaN(endTime) && endTime > startTime) {
                        const durationMs = endTime - startTime;
                        const duration = formatDurationFromMs(durationMs);
                        jobTimeInfo = `Done: ${duration}`;
                    } else {
                        jobTimeInfo = `Done: ${formatTime(job.completed_at)}`;
                    }
                } else {
                    jobTimeInfo = `Done: ${formatTime(job.completed_at || job.started_at)}`;
                }
            } else {
                jobTimeInfo = `${job.status.charAt(0).toUpperCase() + job.status.slice(1)}: ${formatTime(job.completed_at || job.started_at)}`;
            }

            // Format backup type for display
            const backupTypeDisplay = formatBackupType(job.backup_type);

            jobsHTML += `
                <div class="job-item ${job.status}">
                    <div class="job-info">
                        <div class="job-details">
                            <span class="job-id">${job.id}</span>
                            <span class="job-database">${job.database_name}</span>
                            <span class="job-type">${icon} ${backupTypeDisplay}</span>
                            <span class="job-size">💾 ${size}</span>
                        </div>
                        ${job.status === 'running' || job.status === 'optimizing' ? `<div class="job-progress">${jobProgressBar}</div>` : ''}
                        <div class="job-time">
                            <span class="job-status-small">${jobStatusIcon} ${formatStatusText(job.status)}</span>
                            <span class="job-started-small">${jobTimeInfo}</span>
                            <span class="job-actions">${getRunningJobActionButtons(job)}</span>
                        </div>
                    </div>
                </div>
            `;
        });

        jobsHTML += `
                </div>
            </div>
        `;
    });
    
    // Add "Show More" button if there are more groups
    if (showMoreButton) {
        const remainingCount = sortedJobIds.length - maxInitialGroups;
        jobsHTML += `
            <div class="show-more-container">
                <button class="btn btn-secondary btn-sm show-more-btn" onclick="showMoreJobGroups()">
                    📋 Show ${remainingCount} More Job Groups
                </button>
            </div>
        `;
    }

    container.innerHTML = jobsHTML;
    
    // Apply multi-column layout based on number of job groups
    const jobGroups = container.querySelectorAll('.job-group');
    if (jobGroups.length > 1) {
        container.classList.add('multi-column');
        container.classList.remove('single-column');
    } else {
        container.classList.add('single-column');
        container.classList.remove('multi-column');
    }
    
    // Restore scroll positions for expanded job groups
    setTimeout(() => {
        restoreScrollPositions();
        setupScrollTracking();
    }, 50);
    
    // Update job count display
    updateJobCountDisplay(totalRunning, totalJobs, completedJobs);
    
    // Store the full job data for "Show More" functionality
    window.allJobGroups = sortedJobIds;
    window.jobsByJobId = jobsByJobId;
    window.summaries = summaries;
}

function setupScrollTracking() {
    // Add scroll event listeners to all job group details
    const jobGroupDetails = document.querySelectorAll('.job-group-details');
    jobGroupDetails.forEach(detailsElement => {
        const jobId = detailsElement.id.replace('details-', '');
        
        // Remove existing listener if any
        detailsElement.removeEventListener('scroll', detailsElement._scrollHandler);
        
        // Add new scroll handler
        detailsElement._scrollHandler = function() {
            if (window.jobGroupStates.has(jobId)) {
                const state = window.jobGroupStates.get(jobId);
                state.scrollPosition = detailsElement.scrollTop;
                window.jobGroupStates.set(jobId, state);
            }
        };
        
        detailsElement.addEventListener('scroll', detailsElement._scrollHandler);
    });
}

function restoreScrollPositions() {
    // Restore scroll positions for all job groups that have saved positions
    window.jobGroupStates.forEach((state, jobId) => {
        if (state.expanded && state.scrollPosition > 0) {
            const detailsElement = document.getElementById(`details-${jobId}`);
            if (detailsElement) {
                detailsElement.scrollTop = state.scrollPosition;
            }
        }
    });
}

function toggleJobGroup(jobId) {
    const detailsElement = document.getElementById(`details-${jobId}`);
    const collapseBtn = document.querySelector(`[onclick*="toggleJobGroup('${jobId}')"]`);
    const collapseIcon = collapseBtn?.querySelector('.collapse-icon');
    
    if (!detailsElement || !collapseBtn || !collapseIcon) return;
    
    const isCurrentlyExpanded = detailsElement.classList.contains('expanded');
    
    if (isCurrentlyExpanded) {
        // Save scroll position before collapsing
        const scrollPosition = detailsElement.scrollTop;
        window.jobGroupStates.set(jobId, { expanded: false, scrollPosition: scrollPosition });
        
        // Collapse
        detailsElement.classList.remove('expanded');
        detailsElement.classList.add('collapsed');
        collapseBtn.classList.remove('expanded');
        collapseBtn.classList.add('collapsed');
        collapseIcon.textContent = '▶';
    } else {
        // Expand
        detailsElement.classList.remove('collapsed');
        detailsElement.classList.add('expanded');
        collapseBtn.classList.remove('collapsed');
        collapseBtn.classList.add('expanded');
        collapseIcon.textContent = '▼';
        
        // Restore scroll position after a short delay to allow for animation
        setTimeout(() => {
            const savedState = window.jobGroupStates.get(jobId);
            if (savedState && savedState.scrollPosition > 0) {
                detailsElement.scrollTop = savedState.scrollPosition;
            }
        }, 100);
        
        // Update state
        window.jobGroupStates.set(jobId, { expanded: true, scrollPosition: 0 });
    }
}

function showMoreJobGroups() {
    const container = document.getElementById('running-jobs');
    if (!container || !window.allJobGroups || !window.jobsByJobId) return;
    
    const showMoreBtn = container.querySelector('.show-more-btn');
    if (!showMoreBtn) return;
    
    // Hide the "Show More" button
    showMoreBtn.parentElement.style.display = 'none';
    
    // Show all remaining job groups
    const remainingGroups = window.allJobGroups.slice(6); // Skip the first 6 that are already shown
    let additionalHTML = '';
    
    remainingGroups.forEach(jobId => {
        const jobGroup = window.jobsByJobId[jobId];
        const firstJob = jobGroup[0];
        
        // Find summary for this job
        const summary = window.summaries.find(s => s.job_id === jobId);
        
        // Calculate group progress
        const totalProgress = jobGroup.reduce((sum, job) => sum + (job.progress || 0), 0);
        const avgProgress = Math.round(totalProgress / jobGroup.length);
        
        // Calculate group status
        const hasRunning = jobGroup.some(job => job.status === 'running');
        const hasOptimizing = jobGroup.some(job => job.status === 'optimizing');
        const hasFailed = jobGroup.some(job => job.status === 'failed');
        const allDone = jobGroup.every(job => job.status === 'done');
        
        let groupStatus = 'running';
        if (allDone) groupStatus = 'done';
        else if (hasFailed && !hasRunning && !hasOptimizing) groupStatus = 'failed';
        else if (hasRunning || hasOptimizing) groupStatus = 'running';
        
        const statusIcon = getStatusIcon(groupStatus);
        const progressBar = createProgressBar(avgProgress);
        
        // Format time info
        const timeInfo = (hasRunning || hasOptimizing) ? 
            `Started: ${formatTime(firstJob.started_at)}` : 
            `${groupStatus === 'done' ? 'Completed' : 'Failed'}: ${formatTime(firstJob.completed_at || firstJob.started_at)}`;

        // Job group header
        const runningCount = jobGroup.filter(j => j.status === 'running').length;
        const optimizingCount = jobGroup.filter(j => j.status === 'optimizing').length;
        
        // Use backup summary data for success and failed counts instead of counting individual job statuses
        let successCount = 0;
        let failedCount = 0;
        if (summary) {
            successCount = (summary.total_full || 0) + (summary.total_incremental || 0);
            failedCount = summary.total_failed || 0;
            console.log(`Job ${jobId}: Using summary data - success: ${successCount}, failed: ${failedCount}`, summary);
        } else {
            // Fallback to counting individual job statuses if no summary available
            successCount = jobGroup.filter(j => j.status === 'done').length;
            failedCount = jobGroup.filter(j => j.status === 'failed').length;
            console.log(`Job ${jobId}: Using fallback data - success: ${successCount}, failed: ${failedCount}`);
        }
        
        // Check if this job group state is already stored, otherwise default to collapsed
        let isExpanded;
        if (window.jobGroupStates.has(jobId)) {
            isExpanded = window.jobGroupStates.get(jobId).expanded;
        } else {
            // Additional groups are collapsed by default
            isExpanded = false;
            window.jobGroupStates.set(jobId, { expanded: false, scrollPosition: 0 });
        }
        const collapseClass = isExpanded ? 'expanded' : 'collapsed';
        
        additionalHTML += `
            <div class="job-group ${groupStatus}">
                <div class="job-group-header" onclick="toggleJobGroup('${jobId}')">
                    <div class="job-group-info">
                        <span class="job-id">#${jobId}</span>
                        ${runningCount > 0 ? `<span class="job-running">🔄 ${runningCount} running</span>` : ''}
                        ${optimizingCount > 0 ? `<span class="job-optimizing">🔧 ${optimizingCount} optimizing</span>` : ''}
                        ${successCount > 0 ? `<span class="job-success">✅ ${successCount} success</span>` : ''}
                        ${failedCount > 0 ? `<span class="job-failed">❌ ${failedCount} failed</span>` : ''}
                    </div>
                    <div class="job-group-controls">
                        <div class="job-group-time">
                            <span class="job-started">${timeInfo}</span>
                        </div>
                        <button class="collapse-btn ${collapseClass}" onclick="event.stopPropagation(); toggleJobGroup('${jobId}')">
                            <span class="collapse-icon">${isExpanded ? '▼' : '▶'}</span>
                        </button>
                    </div>
                </div>
                <div class="job-group-details ${collapseClass}" id="details-${jobId}">
        `;

        // Sort individual jobs within group
        const sortedJobs = jobGroup.sort((a, b) => {
            const aIsActive = a.status === 'running' || a.status === 'optimizing';
            const bIsActive = b.status === 'running' || b.status === 'optimizing';
            
            if (aIsActive && !bIsActive) return -1;
            if (!aIsActive && bIsActive) return 1;
            
            if (aIsActive && bIsActive) {
                if (a.status === 'running' && b.status === 'optimizing') return -1;
                if (a.status === 'optimizing' && b.status === 'running') return 1;
            }
            
            if (!aIsActive && !bIsActive) {
                const aTime = new Date(a.completed_at || a.started_at).getTime();
                const bTime = new Date(b.completed_at || b.started_at).getTime();
                return bTime - aTime;
            }
            
            return 0;
        });

        // Individual jobs in this group
        sortedJobs.forEach(job => {
            const icon = job.backup_type.includes('full') ? '🔄' : '⚡';
            const jobStatusIcon = getStatusIcon(job.status);
            const jobProgressBar = createProgressBar(job.progress);
            const size = formatSizeFromKB(job.estimated_size_kb || job.actual_size_kb || 0);
            
            // Calculate duration for completed tasks
            let jobTimeInfo;
            if (job.status === 'done' || job.status === 'completed') {
                if (job.started_at && job.completed_at) {
                    const startTime = new Date(job.started_at).getTime();
                    const endTime = new Date(job.completed_at).getTime();
                    
                    if (!isNaN(startTime) && !isNaN(endTime) && endTime > startTime) {
                        const durationMs = endTime - startTime;
                        const duration = formatDurationFromMs(durationMs);
                        jobTimeInfo = `Done: ${duration}`;
                    } else {
                        jobTimeInfo = `Done: ${formatTime(job.completed_at)}`;
                    }
                } else {
                    jobTimeInfo = `Done: ${formatTime(job.completed_at || job.started_at)}`;
                }
            } else {
                jobTimeInfo = `${job.status.charAt(0).toUpperCase() + job.status.slice(1)}: ${formatTime(job.completed_at || job.started_at)}`;
            }

            const backupTypeDisplay = formatBackupType(job.backup_type);

            additionalHTML += `
                <div class="job-item ${job.status}">
                    <div class="job-info">
                        <div class="job-details">
                            <span class="job-id">${job.id}</span>
                            <span class="job-database">${job.database_name}</span>
                            <span class="job-type">${icon} ${backupTypeDisplay}</span>
                            <span class="job-size">💾 ${size}</span>
                        </div>
                        ${job.status === 'running' || job.status === 'optimizing' ? `<div class="job-progress">${jobProgressBar}</div>` : ''}
                        <div class="job-time">
                            <span class="job-status-small">${jobStatusIcon} ${formatStatusText(job.status)}</span>
                            <span class="job-started-small">${jobTimeInfo}</span>
                            <span class="job-actions">${getRunningJobActionButtons(job)}</span>
                        </div>
                    </div>
                </div>
            `;
        });

        additionalHTML += `
                </div>
            </div>
        `;
    });
    
    // Insert the additional HTML before the show more container
    const showMoreContainer = container.querySelector('.show-more-container');
    if (showMoreContainer) {
        showMoreContainer.insertAdjacentHTML('beforebegin', additionalHTML);
    }
    
    // Update layout classes
    const jobGroups = container.querySelectorAll('.job-group');
    if (jobGroups.length > 1) {
        container.classList.add('multi-column');
        container.classList.remove('single-column');
    }
    
    // Setup scroll tracking for the new job groups
    setTimeout(() => {
        setupScrollTracking();
    }, 50);
}

function updateJobCountDisplay(totalRunning, totalJobs, completedJobs) {
    const jobCountElement = document.getElementById('job-count');
    
    if (jobCountElement) {
        jobCountElement.textContent = `${totalRunning}/${totalJobs}`;
    }
    
    // Note: Status dot and text are now managed by WebSocket connection status
    // in updateJobsStatus() function in ws.js
}

function getStatusIcon(status) {
    switch(status) {
        case 'running': return '🔄';
        case 'optimizing': return '🔧';
        case 'done': return '✅';
        case 'completed': return '✅'; // Support both for backward compatibility
        case 'failed': return '❌';
        default: return '❓';
    }
}

function formatBackupType(backupType) {
    switch(backupType) {
        case 'auto-full': return 'Auto Full';
        case 'auto-inc': return 'Auto Incremental';
        case 'force-full': return 'Force Full';
        case 'force-inc': return 'Force Incremental';
        default: return backupType.charAt(0).toUpperCase() + backupType.slice(1);
    }
}

function formatStatusText(status) {
    switch(status) {
        case 'running': return 'Running';
        case 'optimizing': return 'Optimizing';
        case 'done': return 'Completed';
        case 'completed': return 'Completed'; // Support both for backward compatibility
        case 'failed': return 'Failed';
        default: return status.charAt(0).toUpperCase() + status.slice(1);
    }
}

function createProgressBar(progress) {
    return `
        <div class="progress-container">
            <div class="progress-bar">
                <div class="progress-fill" style="width: ${progress}%"></div>
            </div>
        </div>
    `;
}

function getRunningJobActionButtons(job) {
    // No action buttons for running jobs - they are managed centrally
    return '';
}

function updateJobSummary(data) {
    // Handle both old format (array) and new format (object with summaries and jobs)
    let total = 0;
    let completed = 0;
    let running = 0;

    if (!data) {
        // Handle null/undefined data
        total = 0;
        completed = 0;
        running = 0;
    } else if (Array.isArray(data)) {
        // Old format - just jobs array
        total = data.length;
        completed = data.filter(job => job.status === 'done').length;
        // Count all active jobs (running and optimizing - other statuses are done/failed/cancelled)
        running = data.filter(job => job.status === 'running' || job.status === 'optimizing').length;
    } else {
        // New format - object with summaries and jobs
        total = data.total_jobs || 0;
        completed = data.completed_jobs || 0;
        
        // Count active jobs from the jobs array if available
        if (data.jobs && Array.isArray(data.jobs)) {
            running = data.jobs.filter(job => job.status === 'running' || job.status === 'optimizing').length;
        } else {
            // Fallback to total_running if jobs array not available
            running = data.total_running || 0;
        }
        
        // Handle null values from server
        if (total === null) total = 0;
        if (completed === null) completed = 0;
        if (running === null) running = 0;
    }

    // Calculate progress for running jobs header based on backup_summary records
    let progressText = "0/0";
    if (data && data.summaries && Array.isArray(data.summaries)) {
        // Filter running backup summaries
        const runningSummaries = data.summaries.filter(summary => summary.state === 'running');
        
        if (runningSummaries.length > 0) {
            // Calculate numerator: sum of (total_full + total_incremental + total_failed) for all running summaries
            const numerator = runningSummaries.reduce((sum, summary) => {
                const totalFull = parseInt(summary.total_full) || 0;
                const totalIncremental = parseInt(summary.total_incremental) || 0;
                const totalFailed = parseInt(summary.total_failed) || 0;
                return sum + totalFull + totalIncremental + totalFailed;
            }, 0);
            
            // Calculate denominator: sum of total_db_count for all running summaries
            const denominator = runningSummaries.reduce((sum, summary) => {
                return sum + (parseInt(summary.total_db_count) || 0);
            }, 0);
            
            progressText = `${numerator}/${denominator}`;
        }
    }

    const jobCountElement = document.getElementById('job-count');
    if (jobCountElement) {
        jobCountElement.textContent = progressText;
    }
    
    // Update stop button state based on running jobs
    updateStopButtonState(running > 0);
}

function updateStopButtonState(hasRunningJobs) {
    const stopBackupBtn = document.getElementById('stopBackupBtn');
    if (stopBackupBtn) {
        // Enable button when there are running jobs, disable when no running jobs
        stopBackupBtn.disabled = !hasRunningJobs;
        
        // Update button text and styling based on running jobs
        if (hasRunningJobs) {
            stopBackupBtn.textContent = '🛑 Stop All Backups';
            stopBackupBtn.classList.remove('disabled');
        } else {
            stopBackupBtn.textContent = '🛑 Stop All Backups';
            stopBackupBtn.classList.add('disabled');
        }
    }
}

function cancelJob(jobId) {
    showToast(`Cancel job ${jobId} - Function not implemented yet`, 'warning');
}

function viewJobDetails(jobId) {
    showToast(`View details for job ${jobId} - Function not implemented yet`, 'info');
}

function downloadBackup(jobId) {
    showToast(`Download backup ${jobId} - Function not implemented yet`, 'info');
}

function showProgressModal(jobId) {
    const jobIdElement = document.getElementById('job-id');
    const modalElement = document.getElementById('progress-modal');
    
    if (jobIdElement) {
        jobIdElement.textContent = jobId;
    }
    if (modalElement) {
        modalElement.style.display = 'flex';
    }
}

function closeProgressModal() {
    const modalElement = document.getElementById('progress-modal');
    if (modalElement) {
        modalElement.style.display = 'none';
    }
}

function pauseResumeBackup() {
    showToast('Pause/Resume functionality not implemented yet', 'info');
}

function cancelBackupJob() {
    showToast('Cancel backup job functionality not implemented yet', 'info');
}

function updateButtonStates(enabled) {
    const buttons = [
        'loadDatabasesBtn',
        'startBackupBtn'
    ];
    
    buttons.forEach(buttonId => {
        const button = document.getElementById(buttonId);
        if (button) {
            button.disabled = !enabled;
            if (enabled) {
                button.classList.remove('disabled');
            } else {
                button.classList.add('disabled');
            }
        }
    });
    
    // Backup Selected button should be disabled until databases are selected
    const backupSelectedBtn = document.getElementById('backupSelectedBtn');
    if (backupSelectedBtn) {
        backupSelectedBtn.disabled = true; // Always disabled initially
        if (!enabled) {
            backupSelectedBtn.classList.add('disabled');
        } else {
            backupSelectedBtn.classList.remove('disabled');
        }
    }
    
    // Also update the select none button
    const selectNoneBtn = document.querySelector('button[onclick="selectNoneDatabases()"]');
    if (selectNoneBtn) {
        selectNoneBtn.disabled = !enabled;
        if (enabled) {
            selectNoneBtn.classList.remove('disabled');
        } else {
            selectNoneBtn.classList.add('disabled');
        }
    }
    
    // Stop button should be disabled initially (will be enabled when jobs are running)
    const stopBackupBtn = document.getElementById('stopBackupBtn');
    if (stopBackupBtn) {
        stopBackupBtn.disabled = true; // Always disabled initially
        stopBackupBtn.classList.add('disabled');
    }
}

function checkTestResults() {
    fetch('/api/test-results')
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            const results = data.results;
            const buttonsEnabled = results.buttons_enabled;
            
            // Update button states
            updateButtonStates(buttonsEnabled);
            
            // Show appropriate messages
            if (!buttonsEnabled) {
                let errorMessage = 'Database connection or binary validation failed. ';
                if (results.connection_status === 'failed') {
                    errorMessage += `Connection: ${results.connection_message}`;
                } else if (results.binary_status === 'failed') {
                    errorMessage += `Binary validation: ${results.binary_message}`;
                }
                showToast(errorMessage, 'error', 5000);
            } else {
                showToast('Database connection and binary validation successful!', 'success');
            }
        } else {
            // If we can't get test results, disable buttons
            updateButtonStates(false);
            showToast('Unable to verify system status. Buttons disabled for safety.', 'warning');
        }
    })
    .catch(error => {
        console.error('Error checking test results:', error);
        updateButtonStates(false);
        showToast('Error checking system status. Buttons disabled for safety.', 'error');
    });
}

function startCODSpinnerAnimation() {
    const dots = document.querySelector('.cod-loading-dots');
    const percentage = document.querySelector('.cod-percentage');
    
    if (!dots || !percentage) return;
    
    let dotCount = 0;
    let percent = 0;

    // Animate loading dots
    const dotsInterval = setInterval(() => {
        if (!dots.parentElement) {
            clearInterval(dotsInterval);
            return;
        }
        dotCount = (dotCount + 1) % 4;
        dots.textContent = '.'.repeat(dotCount || 1);
    }, 400);

    // Animate percentage counter
    const percentInterval = setInterval(() => {
        if (!percentage.parentElement) {
            clearInterval(percentInterval);
            return;
        }
        percent = (percent + 1) % 101;
        percentage.textContent = percent + '%';
    }, 30);
    
    // Store intervals for cleanup
    window.codSpinnerIntervals = {
        dots: dotsInterval,
        percent: percentInterval
    };
}

function stopCODSpinnerAnimation() {
    if (window.codSpinnerIntervals) {
        clearInterval(window.codSpinnerIntervals.dots);
        clearInterval(window.codSpinnerIntervals.percent);
        window.codSpinnerIntervals = null;
    }
}

