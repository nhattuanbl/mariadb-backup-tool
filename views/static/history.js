// MariaDB Backup Tool - Backup History Management

let currentPage = 1;
let currentLimit = 20;
let currentSearch = '';
let currentStatus = 'all';
let currentDate = '';
let currentSort = 'started_at_desc';
// Use global currentJobId from backup.js instead of declaring here

window.initBackupHistory = function() {
    // Load initial data
    loadBackupHistory();
    
    // Setup event listeners
    const historySearchElement = document.getElementById('history-search');
    const historyStatusFilterElement = document.getElementById('history-status-filter');
    const historyDateFilterElement = document.getElementById('history-date-filter');
    const historySortElement = document.getElementById('history-sort');
    const historyJobFilterElement = document.getElementById('history-job-filter');
    const resetFiltersBtn = document.getElementById('reset-filters-btn');
    const prevPageBtn = document.getElementById('prevPageBtn');
    const nextPageBtn = document.getElementById('nextPageBtn');
    
    if (historySearchElement) {
        historySearchElement.addEventListener('input', function() {
            currentSearch = this.value;
            currentPage = 1;
            loadBackupHistory();
        });
    }
    
    if (historyStatusFilterElement) {
        historyStatusFilterElement.addEventListener('change', function() {
            currentStatus = this.value;
            currentPage = 1;
            loadBackupHistory();
        });
    }
    
    if (historyDateFilterElement) {
        historyDateFilterElement.addEventListener('change', function() {
            currentDate = this.value;
            currentPage = 1;
            loadBackupHistory();
        });
    }
    
    if (historySortElement) {
        historySortElement.addEventListener('change', function() {
            currentSort = this.value;
            currentPage = 1;
            loadBackupHistory();
        });
    }
    
    if (historyJobFilterElement) {
        historyJobFilterElement.addEventListener('input', function() {
            window.currentJobId = this.value;
            currentPage = 1;
            loadBackupHistory();
        });
    }
    
    if (resetFiltersBtn) {
        resetFiltersBtn.addEventListener('click', function() {
            resetFilters();
        });
    }
    
    if (prevPageBtn) {
        prevPageBtn.addEventListener('click', function() {
            if (currentPage > 1) {
                currentPage--;
                loadBackupHistory();
            }
        });
    }
    
    if (nextPageBtn) {
        nextPageBtn.addEventListener('click', function() {
            currentPage++;
            loadBackupHistory();
        });
    }
};

function resetFilters() {
    currentSearch = '';
    currentStatus = 'all';
    currentDate = '';
    currentSort = 'started_at_desc';
    currentPage = 1;
    window.currentJobId = '';
    
    // Reset form elements
    const historySearchElement = document.getElementById('history-search');
    const historyStatusFilterElement = document.getElementById('history-status-filter');
    const historyDateFilterElement = document.getElementById('history-date-filter');
    const historySortElement = document.getElementById('history-sort');
    const historyJobFilterElement = document.getElementById('history-job-filter');
    
    if (historySearchElement) historySearchElement.value = '';
    if (historyStatusFilterElement) historyStatusFilterElement.value = 'all';
    if (historyDateFilterElement) historyDateFilterElement.value = '';
    if (historySortElement) historySortElement.value = 'started_at_desc';
    if (historyJobFilterElement) historyJobFilterElement.value = '';
    
    loadBackupHistory();
}

function loadBackupHistory() {
    const tbody = document.getElementById('backup-history-tbody');
    if (!tbody) return;
    
    tbody.innerHTML = '<tr><td colspan="9" class="text-center text-muted">Loading backup history...</td></tr>';
    
    const params = new URLSearchParams();
    params.append('page', currentPage);
    params.append('limit', currentLimit);
    if (currentSearch) params.append('search', currentSearch);
    if (currentStatus !== 'all') params.append('status', currentStatus);
    if (currentDate) params.append('date', currentDate);
    if (currentSort) params.append('sort', currentSort);
    if (window.currentJobId) params.append('job_id', window.currentJobId);
    
    fetch(`/api/backup/history?${params}`)
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                displayBackupHistory(data.data, data.pagination);
            } else {
                tbody.innerHTML = '<tr><td colspan="9" class="text-center text-error">Error loading backup history: ' + data.error + '</td></tr>';
            }
        })
        .catch(error => {
            console.error('Error loading backup history:', error);
            tbody.innerHTML = '<tr><td colspan="9" class="text-center text-error">Error loading backup history</td></tr>';
        });
}

function displayBackupHistory(jobs, pagination) {
    const tbody = document.getElementById('backup-history-tbody');
    if (!tbody) return;
    
    if (!jobs || jobs.length === 0) {
        tbody.innerHTML = '<tr><td colspan="8" class="text-center text-muted">No backup jobs found</td></tr>';
        updatePagination(pagination);
        return;
    }
    
    let html = '';
    jobs.forEach(job => {
        const typeIcon = getTypeIcon(job.backup_type);
        const duration = getDurationDisplay(job);
        const dbSize = formatSizeFromKB(job.estimated_size_kb);
        const fileSize = formatSizeFromKB(job.actual_size_kb);
        const startTime = formatDateTime(job.started_at);
        const backupPath = job.backup_file_path || '';
        
        html += `
            <tr onclick="openBackupHistoryModal('${job.database_name}')" style="cursor: pointer;">
                <td title="Job ID: ${job.job_id}" class="small-text">${job.id}</td>
                <td class="small-text">${job.database_name}</td>
                <td class="text-center">${typeIcon}</td>
                <td class="small-text">${startTime}</td>
                <td class="small-text">${duration}</td>
                <td class="small-text">${dbSize}</td>
                <td class="small-text">${fileSize}</td>
                <td class="backup-path small-text" title="${backupPath}">${backupPath ? backupPath.substring(0, 50) + (backupPath.length > 50 ? '...' : '') : ''}</td>
            </tr>
        `;
    });
    
    tbody.innerHTML = html;
    updatePagination(pagination);
}

function getTypeIcon(backupType) {
    if (backupType === 'force-full' || backupType === 'auto-full' || backupType.includes('full')) {
        const baseIcon = '🔄';
        return backupType === 'auto-full' ? `🤖${baseIcon}` : baseIcon;
    } else if (backupType === 'force-inc' || backupType === 'auto-inc' || backupType.includes('incremental')) {
        const baseIcon = '⚡';
        return backupType === 'auto-inc' ? `🤖${baseIcon}` : baseIcon;
    }
    return '❓';
}

function getDurationDisplay(job) {
    if (job.status === 'done' && job.completed_at) {
        // Calculate duration from started_at to completed_at
        const startTime = new Date(job.started_at);
        const endTime = new Date(job.completed_at);
        const durationMs = endTime - startTime;
        return formatDurationFromMs(durationMs);
    } else if (job.status !== 'done') {
        // Show status as small tag design with progress if available
        const errorMsg = job.error_message || '';
        const progress = job.progress || 0;
        let displayText = job.status;
        
        // Add progress percentage if status is not success
        if (job.status !== 'done' && progress > 0) {
            displayText += ` ${progress}%`;
        }
        
        return `<span class="status-badge-small ${job.status}" title="${errorMsg}">${displayText}</span>`;
    }
    return '-';
}

function getStatusBadge(status) {
    switch(status) {
        case 'done': return '<span class="status-badge success">Success</span>';
        case 'failed': return '<span class="status-badge error">Failed</span>';
        case 'running': return '<span class="status-badge running">Running</span>';
        default: return '<span class="status-badge unknown">Unknown</span>';
    }
}

// Action buttons removed - no longer needed

function openBackupHistoryModal(databaseName) {
    const modal = document.getElementById('backup-history-modal');
    const title = document.getElementById('backup-history-title');
    const content = document.getElementById('backup-history-content');
    
    if (!modal || !title || !content) {
        console.error('Backup history modal elements not found');
        return;
    }
    
    // Update title
    title.textContent = `📊 Backup History - ${databaseName}`;
    
    // Show loading state
    content.innerHTML = `
        <div class="loading-spinner">
            <div class="spinner"></div>
            <p>Loading backup history for ${databaseName}...</p>
        </div>
    `;
    
    // Show modal
    modal.style.display = 'flex';
    
    // Reset to page 1 and default limit when opening
    currentModalPage = 1;
    currentModalLimit = 10;
    
    // Set dropdown to default value
    const limitDropdown = document.getElementById('limit-dropdown');
    if (limitDropdown) {
        limitDropdown.value = '10';
    }
    
    // Add event listener for limit dropdown
    addLimitDropdownListener();
    
    // Load backup groups
    loadDatabaseBackupGroups(databaseName);
}

function closeBackupHistoryModal() {
    const modal = document.getElementById('backup-history-modal');
    if (modal) {
        modal.style.display = 'none';
    }
}

let currentModalDatabaseName = '';
let currentModalPage = 1;
let currentModalLimit = 10;

function loadDatabaseBackupGroups(databaseName, page = 1) {
    currentModalDatabaseName = databaseName;
    currentModalPage = page;
    

    fetch(`/api/backup/database-groups/${encodeURIComponent(databaseName)}?page=${page}&limit=${currentModalLimit}`)
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                displayBackupGroups(data.groups, data.database_name, data.pagination, data.total_groups);
            } else {
                showError('Failed to load backup groups: ' + data.error);
            }
        })
        .catch(error => {
            console.error('Error loading backup groups:', error);
            showError('Error loading backup groups: ' + error.message);
        });
}

function displayBackupGroups(groups, databaseName, pagination, totalGroups) {
    const content = document.getElementById('backup-history-content');
    
    if (!groups || groups.length === 0) {
        content.innerHTML = `
            <div class="no-data-message">
                <p>No backup history found for database: <strong>${databaseName}</strong></p>
            </div>
        `;
        
        // Update footer for no data
        updateModalFooter({ current_page: 1, total_pages: 1, limit: 10, has_prev: false, has_next: false }, 0);
        return;
    }
    
    let html = `
        <div class="backup-groups-container">
    `;
    
    groups.forEach((group, index) => {
        const fullBackup = group.full_backup;
        const incrementalBackups = group.incremental_backups || [];
        const totalBackups = group.total_backups;
        
        // Format dates and sizes
        const fullBackupDate = formatDateTime(fullBackup.timestamp);
        const fullBackupSize = formatFileSize(fullBackup.file_size);
        const fullBackupDuration = '-'; // No duration available from files
        
        // Use filename directly from filesystem data
        const backupPath = fullBackup.file_path || '';
        const fileName = fullBackup.file_name || 'Unknown file';
        
        html += `
            <div class="backup-group">
                <div class="group-header" onclick="toggleGroup(${index})">
                    <div class="group-info">
                        <span class="backup-type-icon">🔄</span>
                        <span class="backup-type">Full Backup</span>
                        <span class="backup-date">${fullBackupDate}</span>
                        <span class="backup-size">${fullBackupSize}</span>
                        <span class="backup-duration">${fullBackupDuration}</span>
                    </div>
                    <div class="group-controls">
                        <span class="backup-filename" title="${backupPath}">${fileName}</span>
                        <button class="btn btn-sm btn-primary download-btn" data-file-path="${backupPath}" data-file-name="${fileName}" title="Download backup file">
                            ⬇️ Download
                        </button>
                        <button class="btn btn-sm btn-success zip-btn" data-group-index="${index}" title="Download full backup with all incremental backups as ZIP">
                            📦 ZIP
                        </button>
                        <span class="group-toggle" id="toggle-${index}">▼</span>
                    </div>
                </div>
                
                <div class="group-details" id="details-${index}" style="display: none;">
                    <div class="backup-list">
        `;
        
        // Add incremental backups
        if (incrementalBackups.length > 0) {
            incrementalBackups.forEach(incBackup => {
                const incDate = formatDateTime(incBackup.timestamp);
                const incSize = formatFileSize(incBackup.file_size);
                const incDuration = '-'; // No duration available from files
                const incBackupPath = incBackup.file_path || '';
                const incFileName = incBackup.file_name || 'Unknown file';
                
                html += `
                        <div class="backup-item incremental-backup">
                            <div class="backup-info">
                                <span class="backup-type-icon">⚡</span>
                                <span class="backup-type">Incremental</span>
                                <span class="backup-date">${incDate}</span>
                                <span class="backup-size">${incSize}</span>
                                <span class="backup-duration">${incDuration}</span>
                            </div>
                            <div class="backup-controls">
                                <span class="backup-filename" title="${incBackupPath}">${incFileName}</span>
                                <button class="btn btn-sm btn-primary download-btn" data-file-path="${incBackupPath}" data-file-name="${incFileName}" title="Download backup file">
                                    ⬇️ Download
                                </button>
                            </div>
                        </div>
                `;
            });
        } else {
            html += `
                        <div class="no-incremental-message">
                            <p>No incremental backups found for this group</p>
                        </div>
            `;
        }
        
        html += `
                    </div>
                </div>
            </div>
        `;
    });
    
    html += `</div>`;
    
    content.innerHTML = html;
    
    // Add event listeners for download buttons
    addDownloadButtonListeners();
    
    // Update modal footer with pagination
    updateModalFooter(pagination, totalGroups);
}

function toggleGroup(index) {
    const details = document.getElementById(`details-${index}`);
    const toggle = document.getElementById(`toggle-${index}`);
    
    if (details && toggle) {
        if (details.style.display === 'none') {
            details.style.display = 'block';
            toggle.textContent = '▲';
        } else {
            details.style.display = 'none';
            toggle.textContent = '▼';
        }
    }
}

function showError(message) {
    const content = document.getElementById('backup-history-content');
    if (content) {
        content.innerHTML = `
            <div class="error-message">
                <p>❌ ${message}</p>
            </div>
        `;
    }
    
    // Hide backup count in footer
    const backupCountInfo = document.getElementById('backup-count-info');
    if (backupCountInfo) {
        backupCountInfo.style.display = 'none';
    }
}

function updateModalFooter(pagination, totalGroups) {
    const prevBtn = document.getElementById('prev-page-btn');
    const nextBtn = document.getElementById('next-page-btn');
    const countText = document.getElementById('backup-count-text');
    
    if (prevBtn && nextBtn && countText) {
        // Update button states
        prevBtn.disabled = !pagination.has_prev;
        nextBtn.disabled = !pagination.has_next;
        
        // Update count text
        if (totalGroups === 0) {
            countText.textContent = 'No backup files found';
        } else {
            const startItem = ((pagination.current_page - 1) * pagination.limit) + 1;
            const endItem = Math.min(pagination.current_page * pagination.limit, totalGroups);
            countText.textContent = `Showing ${startItem}-${endItem} full backup files of total ${totalGroups}`;
        }
        
        // Update button event listeners
        prevBtn.onclick = pagination.has_prev ? () => loadDatabaseBackupGroups(currentModalDatabaseName, pagination.current_page - 1) : null;
        nextBtn.onclick = pagination.has_next ? () => loadDatabaseBackupGroups(currentModalDatabaseName, pagination.current_page + 1) : null;
    }
}

function addDownloadButtonListeners() {
    const downloadButtons = document.querySelectorAll('.download-btn');
    downloadButtons.forEach(button => {
        button.addEventListener('click', function(e) {
            e.stopPropagation(); // Prevent group toggle
            const filePath = this.getAttribute('data-file-path');
            const fileName = this.getAttribute('data-file-name');
            if (filePath && fileName) {
                downloadBackupFile(filePath, fileName);
            }
        });
    });
    
    // Add event listeners for zip buttons
    const zipButtons = document.querySelectorAll('.zip-btn');
    zipButtons.forEach(button => {
        button.addEventListener('click', function(e) {
            e.stopPropagation(); // Prevent group toggle
            const groupIndex = this.getAttribute('data-group-index');
            if (groupIndex !== null) {
                downloadBackupGroupAsZip(parseInt(groupIndex));
            }
        });
    });
}

function formatFileSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function downloadBackupFile(filePath, fileName) {
    
    // Create a temporary link to download the file
    const link = document.createElement('a');
    link.href = `/api/backup/download-file?path=${encodeURIComponent(filePath)}`;
    link.download = fileName;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    
    // Show toast if function exists
    if (typeof showToast === 'function') {
        showToast(`Downloading ${fileName}...`, 'info');
    } else {
    }
}

function updatePagination(pagination) {
    const pageInfoElement = document.getElementById('pageInfo');
    const prevPageBtn = document.getElementById('prevPageBtn');
    const nextPageBtn = document.getElementById('nextPageBtn');
    
    if (pageInfoElement) {
        const currentPage = pagination.current_page || 1;
        const totalPages = Math.max(1, pagination.total_pages || 1); // Ensure at least 1 page
        const totalCount = pagination.total_count || 0;
        const limit = pagination.limit || 20;
        
        // Calculate showing range
        const startItem = totalCount > 0 ? ((currentPage - 1) * limit) + 1 : 0;
        const endItem = Math.min(currentPage * limit, totalCount);
        
        // Update pagination text
        if (totalCount === 0) {
            pageInfoElement.textContent = 'No records found';
        } else {
            pageInfoElement.textContent = `Page ${currentPage} of ${totalPages} (showing ${startItem}-${endItem} of ${totalCount})`;
        }
    }
    
    if (prevPageBtn) {
        prevPageBtn.disabled = !pagination.has_prev;
    }
    if (nextPageBtn) {
        nextPageBtn.disabled = !pagination.has_next;
    }
}

function addLimitDropdownListener() {
    const limitDropdown = document.getElementById('limit-dropdown');
    if (limitDropdown) {
        // Remove existing listener to avoid duplicates
        limitDropdown.removeEventListener('change', handleLimitChange);
        // Add new listener
        limitDropdown.addEventListener('change', handleLimitChange);
    }
}

function handleLimitChange(event) {
    const newLimit = parseInt(event.target.value);
    if (newLimit !== currentModalLimit && currentModalDatabaseName) {
        currentModalLimit = newLimit;
        currentModalPage = 1; // Reset to page 1 when limit changes
        
        // Reload with new limit
        loadDatabaseBackupGroups(currentModalDatabaseName, 1);
    }
}

function downloadBackupGroupAsZip(groupIndex) {
    
    // Get the group data from the current page
    const groupElement = document.querySelector(`[data-group-index="${groupIndex}"]`).closest('.backup-group');
    if (!groupElement) {
        console.error('Group element not found for index:', groupIndex);
        return;
    }
    
    // Extract file paths from the group
    const fullBackupButton = groupElement.querySelector('.download-btn');
    const fullBackupPath = fullBackupButton.getAttribute('data-file-path');
    const fullBackupName = fullBackupButton.getAttribute('data-file-name');
    
    // Get all incremental backup file paths
    const incButtons = groupElement.querySelectorAll('.backup-item .download-btn');
    const incFilePaths = Array.from(incButtons).map(btn => btn.getAttribute('data-file-path'));
    
    // Create the request data
    const requestData = {
        full_backup_path: fullBackupPath,
        full_backup_name: fullBackupName,
        incremental_paths: incFilePaths,
        database_name: currentModalDatabaseName
    };
    
    
    // Show loading state
    if (typeof showToast === 'function') {
        showToast('Creating ZIP file...', 'info');
    }
    
    // Send request to backend
    fetch('/api/backup/download-group-zip', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(requestData)
    })
    .then(response => {
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        return response.blob();
    })
    .then(blob => {
        // Create download link
        const url = window.URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `${currentModalDatabaseName}_backup_group_${groupIndex + 1}.zip`;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        window.URL.revokeObjectURL(url);
        
        if (typeof showToast === 'function') {
            showToast('ZIP file downloaded successfully!', 'success');
        }
    })
    .catch(error => {
        console.error('Error downloading ZIP:', error);
        if (typeof showToast === 'function') {
            showToast('Error creating ZIP file: ' + error.message, 'error');
        }
    });
}


