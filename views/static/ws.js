// MariaDB Backup Tool - WebSocket Management (Simple & Working)

// Global WebSocket connections
let jobsWebSocket = null;
let systemWebSocket = null;
let logsWebSocket = null;

// Simple connection function
function connectWebSocket(type, path, onMessage) {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}${path}`;
    
    try {
        const ws = new WebSocket(wsUrl);
        
        ws.onopen = function() {
            // Update status indicators based on connection type
            if (type === 'jobs') {
                updateJobsStatus('connected', 'Connected');
            } else if (type === 'system') {
                updateSystemStatus('connected', 'Connected');
            } else if (type === 'logs') {
                updateStreamStatus('connected');
            }
        };
        
        ws.onmessage = onMessage;
        
        ws.onclose = function(event) {
            // Update status indicators based on connection type
            if (type === 'jobs') {
                updateJobsStatus('disconnected', 'Disconnected');
            } else if (type === 'system') {
                updateSystemStatus('disconnected', 'Disconnected');
            } else if (type === 'logs') {
                updateStreamStatus('disconnected');
            }
            // Simple reconnect after 2 seconds
            setTimeout(() => {
                connectWebSocket(type, path, onMessage);
            }, 2000);
        };
        
        ws.onerror = function(error) {
            console.error(`${type} WebSocket error:`, error);
            // Update status indicators based on connection type
            if (type === 'jobs') {
                updateJobsStatus('disconnected', 'Error');
            } else if (type === 'system') {
                updateSystemStatus('disconnected', 'Error');
            } else if (type === 'logs') {
                updateStreamStatus('disconnected');
            }
        };
        
        return ws;
    } catch (error) {
        console.error(`Failed to create ${type} WebSocket:`, error);
        return null;
    }
}

// Connect Jobs WebSocket
function connectJobsWebSocket() {
    if (jobsWebSocket && jobsWebSocket.readyState === WebSocket.OPEN) {
        return;
    }
    
    // Set connecting status
    updateJobsStatus('connecting', 'Connecting...');
    
    jobsWebSocket = connectWebSocket('jobs', '/ws/jobs', function(event) {
        try {
            const message = JSON.parse(event.data);
            if (message.type === 'jobs_update') {
                if (typeof displayRunningJobs === 'function') {
                    displayRunningJobs(message.data || []);
                }
                if (typeof updateJobSummary === 'function') {
                    updateJobSummary(message.data || []);
                }
            }
        } catch (error) {
            console.error('Error parsing jobs WebSocket message:', error);
        }
    });
}

// Connect System WebSocket
function connectSystemWebSocket() {
    if (systemWebSocket && systemWebSocket.readyState === WebSocket.OPEN) {
        return;
    }
    
    // Set connecting status
    updateSystemStatus('connecting', 'Connecting...');
    
    systemWebSocket = connectWebSocket('system', '/ws/system', function(event) {
        try {
            const message = JSON.parse(event.data);
            if (message.type === 'system_update') {
                if (typeof updateSystemStats === 'function') {
                    updateSystemStats(message.data);
                }
            }
        } catch (error) {
            console.error('Error parsing system WebSocket message:', error);
        }
    });
}

// Connect Logs WebSocket
function connectLogsWebSocket() {
    if (logsWebSocket && logsWebSocket.readyState === WebSocket.OPEN) {
        return;
    }
    
    // Set connecting status
    updateStreamStatus('connecting');
    
    // Only connect for current day
    const today = new Date();
    const year = today.getFullYear();
    const month = String(today.getMonth() + 1).padStart(2, '0');
    const day = String(today.getDate()).padStart(2, '0');
    const todayStr = `${year}-${month}-${day}`;
    
    const logDateElement = document.getElementById('log-date');
    const selectedDate = logDateElement ? logDateElement.value || todayStr : todayStr;
    
    if (selectedDate !== todayStr) {
        return;
    }
    
    const wsUrl = `/ws/logs?date=${selectedDate}`;
    
    logsWebSocket = connectWebSocket('logs', wsUrl, function(event) {
        try {
            const data = JSON.parse(event.data);
            
            switch (data.type) {
                case 'logs_connected':
                    // Update status to connected
                    updateStreamStatus('connected');
                    // Clear loading spinner
                    const logContainer = document.getElementById('log-entries');
                    if (logContainer) {
                        const loadingElement = logContainer.querySelector('.log-loading');
                        if (loadingElement) {
                            loadingElement.remove();
                        }
                    }
                    break;
                case 'logs_update':
                    if (typeof addNewLog === 'function') {
                        addNewLog(data.log);
                    }
                    break;
            }
        } catch (error) {
            console.error('Error parsing logs WebSocket message:', error);
        }
    });
}

// Disconnect functions
function disconnectJobsWebSocket() {
    if (jobsWebSocket) {
        jobsWebSocket.close();
        jobsWebSocket = null;
    }
}

function disconnectSystemWebSocket() {
    if (systemWebSocket) {
        systemWebSocket.close();
        systemWebSocket = null;
    }
}

function disconnectLogsWebSocket() {
    if (logsWebSocket) {
        logsWebSocket.close();
        logsWebSocket = null;
    }
}

// Status update functions
function updateJobsStatus(status, text) {
    const statusDot = document.querySelector('#jobs-status .status-dot');
    const statusText = document.querySelector('#jobs-status .status-text');
    
    if (statusDot && statusText) {
        statusDot.className = `status-dot ${status}`;
        statusText.textContent = text;
    }
}

function updateSystemStatus(status, text) {
    const statusDot = document.getElementById('monitor-status');
    const statusText = document.getElementById('monitor-text');
    
    if (statusDot && statusText) {
        statusDot.className = `status-dot ${status}`;
        statusText.textContent = text;
    }
}

function updateStreamStatus(status) {
    const statusElement = document.getElementById('stream-status');
    if (!statusElement) return;
    
    const dot = statusElement.querySelector('.status-dot');
    const text = document.getElementById('stream-status-text');
    
    if (status === true || status === 'connected') {
        dot.className = 'status-dot connected';
        text.textContent = 'Connected';
    } else if (status === 'connecting') {
        dot.className = 'status-dot connecting';
        text.textContent = 'Connecting...';
    } else {
        dot.className = 'status-dot disconnected';
        text.textContent = 'Disconnected';
    }
}

// System stats update
function updateSystemStats(data) {
    // Update CPU
    const cpuPercent = Math.round(data.cpu_percent || 0);
    const cpuCount = data.cpu_count || 0;
    const cpuCores = data.cpu_cores || 0;
    const cpuThreads = data.cpu_threads || 0;
    const cpuElement = document.getElementById('cpu-usage');
    const cpuFill = document.getElementById('cpu-fill');
    
    if (cpuElement) {
        // Format: "1% (1CPU 16Cores 32Threads)"
        const cpuText = `${cpuPercent}% (${cpuCount}CPU ${cpuCores}Cores ${cpuThreads}Threads)`;
        cpuElement.textContent = cpuText;
    }
    if (cpuFill) {
        cpuFill.style.width = cpuPercent + '%';
        cpuFill.className = `monitor-fill cpu-${getCpuClass(cpuPercent)}`;
    }

    // Update RAM
    const ramPercent = Math.round(data.memory_percent || 0);
    const ramUsed = data.memory_used || 0;
    const ramTotal = data.memory_total || 0;
    const ramElement = document.getElementById('ram-usage');
    const ramFill = document.getElementById('ram-fill');
    
    if (ramElement) {
        ramElement.textContent = `${ramPercent}% (${ramUsed}GB / ${ramTotal}GB)`;
    }
    if (ramFill) {
        ramFill.style.width = ramPercent + '%';
        ramFill.className = `monitor-fill ram-${getRamClass(ramPercent)}`;
    }

    // Update Disk
    const diskPercent = Math.round(data.disk_percent || 0);
    const diskUsed = data.disk_used || 0;
    const diskTotal = data.disk_total || 0;
    const diskElement = document.getElementById('disk-usage');
    const diskFill = document.getElementById('disk-fill');
    
    if (diskElement) {
        diskElement.textContent = `${diskPercent}% (${diskUsed}GB / ${diskTotal}GB)`;
    }
    if (diskFill) {
        diskFill.style.width = diskPercent + '%';
        diskFill.className = `monitor-fill disk-${getDiskClass(diskPercent)}`;
    }
}

// Helper functions for system stats
function getCpuClass(percent) {
    if (percent > 80) return 'high';
    if (percent > 60) return 'medium';
    return 'low';
}

function getRamClass(percent) {
    if (percent > 85) return 'high';
    if (percent > 70) return 'medium';
    return 'low';
}

function getDiskClass(percent) {
    if (percent > 90) return 'high';
    if (percent > 75) return 'medium';
    return 'low';
}

// Initialize connections based on current page
function initializeWebSockets() {
    const currentPath = window.location.pathname;
    
    if (currentPath === '/dashboard' || currentPath === '/' || currentPath === '/backup') {
        connectJobsWebSocket();
        connectSystemWebSocket();
    }
    
    if (currentPath === '/backup') {
        connectLogsWebSocket();
    }
}

// Cleanup on page unload
window.addEventListener('beforeunload', function() {
    disconnectJobsWebSocket();
    disconnectSystemWebSocket();
    disconnectLogsWebSocket();
});

// Initialize when page loads
document.addEventListener('DOMContentLoaded', function() {
    setTimeout(initializeWebSockets, 1000);
});
