// MariaDB Backup Tool - Log Stream Management
let isStreamPaused = false;
let isViewCleared = false;
let currentFilters = {
    level: 'IMPORTANT',
    date: '',
    search: ''
};
let isCurrentDay = true;
let seenLogIds = new Set();
let allLogs = [];
let isLoadingMore = false;
let hasMoreLogs = false;

// Performance optimization: limit displayed logs to prevent browser lag
const MAX_DISPLAYED_LOGS = 1500; // Show max 1500 log entries in DOM
const MAX_STORED_LOGS = 2000; // Keep max 2000 logs in memory for filtering

function generateLogId(log) {
    return `${log.timestamp}-${log.level}-${log.message}`.replace(/\s+/g, '_');
}

function initLogStream() {
    const today = new Date();
    const year = today.getFullYear();
    const month = String(today.getMonth() + 1).padStart(2, '0');
    const day = String(today.getDate()).padStart(2, '0');
    const todayStr = `${year}-${month}-${day}`;
    
    
    const logDateElement = document.getElementById('log-date');
    if (logDateElement) {
        logDateElement.value = todayStr;
    }
    currentFilters.date = todayStr;
    isCurrentDay = true;
    
    const logLevelElement = document.getElementById('log-level');
    if (logLevelElement) {
        logLevelElement.value = 'IMPORTANT';
    }
    currentFilters.level = 'IMPORTANT';
    
    const deleteButton = document.getElementById('delete-log-file');
    if (deleteButton) {
        deleteButton.disabled = false;
        deleteButton.title = 'Clear log file content for today (file will be kept)';
        deleteButton.style.opacity = '1';
    }
    
    loadInitialLogsForDate(todayStr);
    
    setupLogStreamEventListeners();
}

function loadInitialLogsForDate(date) {
    
    const container = document.getElementById('log-entries');
    if (container) {
        container.innerHTML = '<div class="log-loading"><div class="spinner"></div><p>Loading logs...</p></div>';
    }
    
    // Reset loading state
    isLoadingMore = false;
    hasMoreLogs = true;
    
    const params = new URLSearchParams();
    params.append('date', date);
    params.append('limit', MAX_STORED_LOGS.toString());
    params.append('offset', '0');
    
    fetch(`/api/logs/stream?${params}`)
        .then(response => {
            const contentType = response.headers.get('content-type');
            if (contentType && contentType.includes('text/html')) {
                throw new Error('Authentication required - redirected to login page');
            }
            
            return response.json();
        })
        .then(data => {
            if (data.success) {
                allLogs = data.logs || [];
                // Check if there might be more logs (if we got exactly the limit, there might be more)
                if (data.logs && data.logs.length < MAX_STORED_LOGS) {
                    hasMoreLogs = false;
                }
                displayAllLogs();
                // Setup scroll listener after initial load
                setupScrollListener();
            } else {
                console.error('Failed to load initial logs:', data.error);
                const container = document.getElementById('log-entries');
                if (container) {
                    container.innerHTML = '<div class="log-empty">Failed to load logs: ' + data.error + '</div>';
                }
            }
        })
        .catch(error => {
            console.error('Error loading initial logs:', error);
            
            let errorMessage = error.message;
            if (error.message.includes('Authentication required')) {
                errorMessage = 'Authentication required. Please refresh the page and log in again.';
            }
            
            const container = document.getElementById('log-entries');
            if (container) {
                container.innerHTML = '<div class="log-empty">' + errorMessage + '</div>';
            }
        });
}

function displayAllLogs() {
    const container = document.getElementById('log-entries');
    const logCount = document.getElementById('log-count');
    
    if (!container) return;
    
    // Clear container
    container.innerHTML = '';
    
    if (allLogs.length === 0) {
        container.innerHTML = '<div class="log-empty">No log entries found</div>';
        if (logCount) logCount.textContent = '0 entries';
        return;
    }
    
    const filteredLogs = applyCurrentFilters(allLogs);
    
    if (filteredLogs.length === 0) {
        container.innerHTML = '<div class="log-empty">No log entries match current filters</div>';
        if (logCount) logCount.textContent = '0 entries';
        return;
    }
    
    filteredLogs.sort((a, b) => {
        const timeA = parseLogTimestamp(a);
        const timeB = parseLogTimestamp(b);
        return timeB - timeA;
    });
    
    // Performance optimization: limit displayed logs to prevent browser lag
    const logsToDisplay = filteredLogs.slice(0, MAX_DISPLAYED_LOGS);
    const totalFilteredCount = filteredLogs.length;
    
    logsToDisplay.forEach(log => {
        const logEntry = createLogEntry(log);
        container.appendChild(logEntry);
    });
    
    if (logCount) {
        if (totalFilteredCount > MAX_DISPLAYED_LOGS) {
            logCount.textContent = `${logsToDisplay.length} of ${totalFilteredCount} entries (showing latest ${MAX_DISPLAYED_LOGS})`;
            logCount.title = `Performance optimized: Only showing the latest ${MAX_DISPLAYED_LOGS} entries to prevent browser lag`;
        } else {
            logCount.textContent = `${logsToDisplay.length} entries`;
            logCount.title = '';
        }
    }
    
    scrollToTop();
}

function addNewLog(log) {
    allLogs.unshift(log);
    
    // Performance optimization: limit stored logs to prevent memory issues
    if (allLogs.length > MAX_STORED_LOGS) {
        allLogs = allLogs.slice(0, MAX_STORED_LOGS);
    }
    
    if (isViewCleared) {
        return;
    }
    
    if (isStreamPaused) {
        return;
    }
    
    const filteredLogs = applyCurrentFilters([log]);
    if (filteredLogs.length > 0) {
        const container = document.getElementById('log-entries');
        if (container) {
            const loadingElements = container.querySelectorAll('.log-loading, .log-empty');
            loadingElements.forEach(el => el.remove());
            
            const logEntry = createLogEntry(log);
            container.insertBefore(logEntry, container.firstChild);
            
            // Performance optimization: remove old DOM elements if we exceed the limit
            const logEntries = container.querySelectorAll('.log-entry');
            if (logEntries.length > MAX_DISPLAYED_LOGS) {
                // Remove the oldest entries (at the bottom)
                const entriesToRemove = logEntries.length - MAX_DISPLAYED_LOGS;
                for (let i = 0; i < entriesToRemove; i++) {
                    const lastEntry = logEntries[logEntries.length - 1 - i];
                    if (lastEntry) {
                        lastEntry.remove();
                    }
                }
            }
            
            const logCount = document.getElementById('log-count');
            if (logCount) {
                const currentCount = parseInt(logCount.textContent.split(' ')[0]) || 0;
                const totalFilteredCount = applyCurrentFilters(allLogs).length;
                
                if (totalFilteredCount > MAX_DISPLAYED_LOGS) {
                    logCount.textContent = `${Math.min(currentCount + 1, MAX_DISPLAYED_LOGS)} of ${totalFilteredCount} entries (showing latest ${MAX_DISPLAYED_LOGS})`;
                    logCount.title = `Performance optimized: Only showing the latest ${MAX_DISPLAYED_LOGS} entries to prevent browser lag`;
                } else {
                    logCount.textContent = `${currentCount + 1} entries`;
                    logCount.title = '';
                }
            }
            
            scrollToTop();
        }
    }
}

function setupLogStreamEventListeners() {
    const pauseStreamBtn = document.getElementById('pause-stream');
    const clearLogsBtn = document.getElementById('clear-logs');
    const deleteLogFileBtn = document.getElementById('delete-log-file');
    const scrollToBottomBtn = document.getElementById('scroll-to-bottom');
    
    if (pauseStreamBtn) pauseStreamBtn.addEventListener('click', toggleStreamPause);
    if (clearLogsBtn) clearLogsBtn.addEventListener('click', clearLogDisplay);
    if (deleteLogFileBtn) deleteLogFileBtn.addEventListener('click', deleteLogFile);
    if (scrollToBottomBtn) scrollToBottomBtn.addEventListener('click', scrollToBottom);
    
    const logDateElement = document.getElementById('log-date');
    const logLevelElement = document.getElementById('log-level');
    const searchLogsElement = document.getElementById('search-logs');
    
    if (logDateElement) logDateElement.addEventListener('change', handleDateChange);
    if (logLevelElement) logLevelElement.addEventListener('change', handleFilterChange);
    if (searchLogsElement) searchLogsElement.addEventListener('input', handleFilterChange);
}

function handleDateChange() {
    const selectedDate = document.getElementById('log-date').value;
    
    // Calculate today's date using local timezone
    const today = new Date();
    const year = today.getFullYear();
    const month = String(today.getMonth() + 1).padStart(2, '0');
    const day = String(today.getDate()).padStart(2, '0');
    const todayStr = `${year}-${month}-${day}`;
    
    currentFilters.date = selectedDate;
    isCurrentDay = (selectedDate === todayStr);
    
    // Reset cleared state when date changes
    isViewCleared = false;
    
    
    // Update delete button state
    const deleteButton = document.getElementById('delete-log-file');
    if (deleteButton) {
        if (isCurrentDay) {
            deleteButton.disabled = false;
            deleteButton.title = 'Clear log file content for today (file will be kept)';
            deleteButton.style.opacity = '1';
        } else {
            deleteButton.disabled = false;
            deleteButton.title = 'Delete log file for selected date';
            deleteButton.style.opacity = '1';
        }
    }
    
    // If not current day, disconnect WebSocket (no need for real-time updates)
    if (!isCurrentDay) {
        disconnectLogsWebSocket();
    } else {
        // If current day, reconnect WebSocket
        disconnectLogsWebSocket();
        // connectLogsWebSocket(); // Handled by ws.js
    }
    
    // Load logs for the selected date
    loadInitialLogsForDate(selectedDate);
    
    // Setup scroll listener after date changes
    setupScrollListener();
}

function handleFilterChange() {
    const logLevelElement = document.getElementById('log-level');
    const searchLogsElement = document.getElementById('search-logs');
    
    if (logLevelElement) currentFilters.level = logLevelElement.value;
    if (searchLogsElement) currentFilters.search = searchLogsElement.value;
    
    // Reset cleared state when filters change (user wants to see filtered results)
    isViewCleared = false;
    
    // Re-display all logs with new filters
    displayAllLogs();
}

function createLogEntry(log) {
    const entry = document.createElement('div');
    entry.className = `log-entry log-${log.color}`;
    entry.dataset.timestamp = log.timestamp;
    entry.dataset.fullTimestamp = log.fullTimestamp || parseLogTimestamp(log);
    
    entry.innerHTML = `
        <div class="log-time">${log.timestamp}</div>
        <div class="log-level log-level-${log.color}">${log.level}</div>
        <div class="log-message">${escapeHtml(log.message)}</div>
    `;
    
    return entry;
}

function parseLogTimestamp(log) {
    // Use fullTimestamp if available (nanoseconds since epoch), otherwise parse HH:MM:SS
    if (log.fullTimestamp) {
        return parseInt(log.fullTimestamp, 10);
    }
    
    // Fallback to parsing HH:MM:SS format
    const timestamp = log.timestamp || '';
    const parts = timestamp.split(':');
    if (parts.length === 3) {
        const hours = parseInt(parts[0], 10);
        const minutes = parseInt(parts[1], 10);
        const seconds = parseInt(parts[2], 10);
        return (hours * 3600 + minutes * 60 + seconds) * 1000;
    }
    return 0; // Fallback for invalid timestamps
}

function applyCurrentFilters(logs) {
    return logs.filter(log => {
        // Level filter
        if (currentFilters.level) {
            if (currentFilters.level === 'IMPORTANT') {
                // Show ERROR, WARN, and INFO levels (exclude DEBUG)
                if (!['ERROR', 'WARN', 'WARNING', 'INFO'].includes(log.level)) {
                    return false;
                }
            } else if (log.level !== currentFilters.level) {
                return false;
            }
        }
        
        // Search filter
        if (currentFilters.search && !log.message.toLowerCase().includes(currentFilters.search.toLowerCase())) {
            return false;
        }
        
        return true;
    });
}

function toggleStreamPause() {
    isStreamPaused = !isStreamPaused;
    const button = document.getElementById('pause-stream');
    
    if (button) {
        if (isStreamPaused) {
            button.textContent = '▶️ Resume';
            button.classList.add('paused');
            showToast('Log stream paused', 'info');
        } else {
            button.textContent = '⏸️ Pause';
            button.classList.remove('paused');
            showToast('Log stream resumed', 'success');
        }
    }
}

function deleteLogFile() {
    const selectedDate = document.getElementById('log-date').value;
    
    if (!selectedDate) {
        showToast('Please select a date first', 'warning');
        return;
    }
    
    // Calculate today's date using local timezone
    const today = new Date();
    const year = today.getFullYear();
    const month = String(today.getMonth() + 1).padStart(2, '0');
    const day = String(today.getDate()).padStart(2, '0');
    const todayStr = `${year}-${month}-${day}`;
    
    const isCurrentDay = selectedDate === todayStr;
    
    // Show appropriate confirmation dialog based on action
    let confirmMessage;
    if (isCurrentDay) {
        confirmMessage = `Are you sure you want to clear the log file content for ${selectedDate}? The file will be kept but all content will be removed. This action cannot be undone.`;
    } else {
        confirmMessage = `Are you sure you want to delete the log file for ${selectedDate}? This action cannot be undone.`;
    }
    
    if (!confirm(confirmMessage)) {
        return;
    }
    
    // Make API call to delete/clear the log file
    fetch('/api/logs/delete', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({
            date: selectedDate
        })
    })
    .then(response => response.json())
    .then(data => {
        if (data.success) {
            // Show appropriate success message based on action
            if (data.action === 'cleared') {
                showToast('Log file content cleared successfully', 'success');
            } else {
                showToast('Log file deleted successfully', 'success');
            }
            
            // Clear the display and reload logs
            const logEntries = document.getElementById('log-entries');
            if (logEntries) {
                if (data.action === 'cleared') {
                    logEntries.innerHTML = '<div class="log-empty">Log file content cleared</div>';
                } else {
                    logEntries.innerHTML = '<div class="log-empty">Log file deleted</div>';
                }
            }
            const logCount = document.getElementById('log-count');
            if (logCount) {
                logCount.textContent = '0 entries';
            }
            // Clear stored logs
            allLogs = [];
        } else {
            // Show specific error message
            let errorMessage = data.error;
            let errorType = 'error';
            
            // Check for specific error types to show appropriate messages
            if (errorMessage.includes('currently being used by the logging system')) {
                errorMessage = 'Cannot delete today\'s log file while the application is running. Please select a different date or restart the application.';
                errorType = 'warning';
            } else if (errorMessage.includes('currently being used by another process')) {
                errorMessage = 'The log file is currently in use. Please try again later or restart the application.';
                errorType = 'warning';
            }
            
            showToast(errorMessage, errorType, 8000);
        }
    })
    .catch(error => {
        console.error('Error deleting log file:', error);
        showToast('Error deleting log file', 'error');
    });
}

function clearLogDisplay() {
    // Set cleared state to prevent WebSocket updates from repopulating
    isViewCleared = true;
    
    // Clear the log display but keep streaming active
    const logEntries = document.getElementById('log-entries');
    if (logEntries) {
        logEntries.innerHTML = `
            <div class="log-empty">
                Log display cleared - streaming continues
                <br><br>
                <button type="button" class="btn btn-sm btn-primary" onclick="resumeLogStream()">
                    ▶️ Resume Stream
                </button>
                <button type="button" class="btn btn-sm btn-secondary" onclick="reloadCurrentDate()">
                    🔄 Reload Current Date
                </button>
            </div>
        `;
    }
    
    const logCount = document.getElementById('log-count');
    if (logCount) {
        logCount.textContent = '0 entries';
    }
    
    // Show a brief toast message
    showToast('Log view cleared - click Resume Stream to continue receiving updates', 'info', 5000);
}

function resumeLogStream() {
    // Reset cleared state to allow WebSocket updates
    isViewCleared = false;
    
    // Re-display all logs
    displayAllLogs();
    
    // Show toast message
    showToast('Log stream resumed - new messages will appear automatically', 'success', 3000);
}

function reloadCurrentDate() {
    // Reset cleared state
    isViewCleared = false;
    
    // Reload logs for current date
    const selectedDate = document.getElementById('log-date').value;
    loadInitialLogsForDate(selectedDate);
    
    // Show toast message
    showToast('Reloading logs for current date...', 'info', 2000);
}

function scrollToTop() {
    const container = document.getElementById('log-entries');
    if (container) {
        container.scrollTop = 0;
    }
}

function scrollToBottom() {
    const container = document.getElementById('log-entries');
    if (container) {
        container.scrollTop = container.scrollHeight;
    }
}

function isScrollAtTop(container) {
    const threshold = 5; // Allow 5px tolerance
    return container.scrollTop <= threshold;
}

function isScrollAtBottom(container) {
    const threshold = 5; // Allow 5px tolerance
    return container.scrollTop + container.clientHeight >= container.scrollHeight - threshold;
}

function setupScrollListener() {
    const container = document.getElementById('log-entries');
    if (!container) return;
    
    // Remove existing listener if any
    container.onscroll = null;
    
    // Add scroll listener
    container.onscroll = function() {
        // Only load more if viewing historical logs (not current day)
        if (isCurrentDay) return;
        
        // Only load if not already loading and there might be more logs
        if (isLoadingMore || !hasMoreLogs) return;
        
        // Check if user scrolled to bottom
        if (isScrollAtBottom(container)) {
            loadMoreLogs();
        }
    };
}

function loadMoreLogs() {
    if (isLoadingMore || !hasMoreLogs) return;
    
    isLoadingMore = true;
    
    // Show loading indicator at bottom
    const container = document.getElementById('log-entries');
    if (!container) {
        isLoadingMore = false;
        return;
    }
    
    // Add loading indicator
    const loadingIndicator = document.createElement('div');
    loadingIndicator.className = 'log-loading-more';
    loadingIndicator.innerHTML = '<div class="spinner-small"></div><p>Loading more logs...</p>';
    container.appendChild(loadingIndicator);
    
    // Calculate next offset (current number of logs)
    const offset = allLogs.length;
    
    // Get the current date filter
    const selectedDate = document.getElementById('log-date').value;
    
    const params = new URLSearchParams();
    params.append('date', selectedDate);
    params.append('limit', '500'); // Load 500 more logs
    params.append('offset', offset.toString());
    
    fetch(`/api/logs/stream?${params}`)
        .then(response => response.json())
        .then(data => {
            loadingIndicator.remove();
            
            if (data.success && data.logs && data.logs.length > 0) {
                // Append new logs to allLogs array (oldest logs, since we're paginating)
                allLogs = allLogs.concat(data.logs);
                
                // Check if there might be more logs
                if (data.logs.length < 500) {
                    hasMoreLogs = false;
                }
                
                // Re-display all logs with the new additions
                displayAllLogs();
                
                // Restore scroll position (approximate)
                setTimeout(() => {
                    container.scrollTop = container.scrollHeight * 0.5; // Try to maintain position
                }, 10);
                
                console.log(`Loaded ${data.logs.length} more logs. Total: ${allLogs.length}`);
            } else {
                // No more logs available
                hasMoreLogs = false;
                console.log('No more logs to load');
            }
            
            isLoadingMore = false;
        })
        .catch(error => {
            console.error('Error loading more logs:', error);
            loadingIndicator.remove();
            isLoadingMore = false;
            hasMoreLogs = false;
        });
}
