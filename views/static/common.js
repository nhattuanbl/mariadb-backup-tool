// MariaDB Backup Tool - Common JavaScript Functions

document.addEventListener('DOMContentLoaded', function() {
    const navIcon = document.querySelector('.nav-icon');
    const navTitle = document.querySelector('.nav-brand h1');

    if (navIcon) {
        navIcon.style.cursor = 'pointer';
        navIcon.addEventListener('click', function() {
            window.open('https://github.com/nhattuanbl/mariadb-backup-tool', '_blank');
        });
    }

    if (navTitle) {
        navTitle.style.cursor = 'pointer';
        navTitle.addEventListener('click', function() {
            window.open('https://github.com/nhattuanbl/mariadb-backup-tool', '_blank');
        });
    }
});

function showToast(message, type = 'info', duration = null) {
    // Get or create toast container
    let container = document.getElementById('toast-container');
    if (!container) {
        container = document.createElement('div');
        container.id = 'toast-container';
        document.body.appendChild(container);
    }
    
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.innerHTML = message.replace(/\n/g, '<br>');
    
    // Set duration based on type if not specified
    if (duration === null) {
        if (type === 'error') {
            duration = 8000; // 8 seconds for errors
        } else if (type === 'warning') {
            duration = 6000; // 6 seconds for warnings
        } else if (type === 'success') {
            duration = 4000; // 4 seconds for success
        } else {
            duration = 3000; // 3 seconds for info
        }
    }

    // Add toast to container
    container.appendChild(toast);

    // Auto remove after duration
    setTimeout(() => {
        if (container.contains(toast)) {
            toast.style.animation = 'slideOut 0.3s ease';
            setTimeout(() => {
                if (container.contains(toast)) {
                    container.removeChild(toast);
                }
            }, 300);
        }
    }, duration);
}

function formatBytes(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatDuration(seconds) {
    if (!seconds) return '0s';
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;
    
    if (hours > 0) {
        return `${hours}h ${minutes}m ${secs}s`;
    } else if (minutes > 0) {
        return `${minutes}m ${secs}s`;
    } else {
        return `${secs}s`;
    }
}

function formatDateTime(dateStr) {
    if (!dateStr) return 'N/A';
    const date = new Date(dateStr);
    return date.toLocaleDateString() + ' ' + date.toLocaleTimeString();
}

function formatSizeFromKB(sizeKB) {
    // Handle null, undefined, or invalid values
    if (!sizeKB || isNaN(sizeKB) || sizeKB < 0) return '0 KB';
    
    if (sizeKB >= 1024 * 1024) {
        return `${(sizeKB / (1024 * 1024)).toFixed(1)} GB`;
    } else if (sizeKB >= 1024) {
        return `${(sizeKB / 1024).toFixed(1)} MB`;
    } else {
        return `${Math.round(sizeKB)} KB`;
    }
}

function formatTime(timeString) {
    if (!timeString) return 'Unknown';
    const date = new Date(timeString);
    return date.toLocaleTimeString();
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatDurationFromMs(ms) {
    // Handle invalid or negative values
    if (!ms || isNaN(ms) || ms < 0) return '0s';
    
    if (ms < 1000) return '0s';
    if (ms < 60000) return Math.round(ms / 1000) + 's';
    if (ms < 3600000) {
        const minutes = Math.floor(ms / 60000);
        const seconds = Math.round((ms % 60000) / 1000);
        return minutes + 'm ' + seconds + 's';
    }
    const hours = Math.floor(ms / 3600000);
    const minutes = Math.round((ms % 3600000) / 60000);
    return hours + 'h ' + minutes + 'm';
}

// Toast animations are handled by CSS


