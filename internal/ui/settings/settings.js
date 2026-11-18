// Global state for firmware update
let firmwareUpdateState = {
    ws: null,
    uploadInProgress: false,
    currentProgress: 0,
    currentStatus: 'idle',
    error: null
};

// Configuration constants
const FIRMWARE_CONFIG = {
    maxFileSize: 500 * 1024 * 1024, // 500MB in bytes
    maxFileSizeDisplay: '500 Mo',
    validExtensions: ['.swu', '.bin', '.img'],
    validMimeTypes: ['application/octet-stream', 'application/bin', 'application/x-bin']
};

function switchTab(event, tabName) {
    const tabButtons = document.querySelectorAll('.nav-tab');
    document.querySelectorAll('.tab-content').forEach(tab => {
        tab.classList.remove('active');
    });
    tabButtons.forEach(tab => {
        tab.classList.remove('active');
        tab.setAttribute('aria-selected', 'false');
        tab.setAttribute('tabindex', '-1');
    });
    
    const panel = document.getElementById(tabName);
    const selectedTab = event.currentTarget;
    panel.classList.add('active');
    selectedTab.classList.add('active');
    selectedTab.setAttribute('aria-selected', 'true');
    selectedTab.setAttribute('tabindex', '0');
    
    if (tabName === 'info') {
        loadSystemInfo();
    } else if (tabName === 'settings') {
        loadSettings();
    } else if (tabName === 'firmware') {
        loadFirmwareInfo();
    }
}

function navigateTabs(event) {
    const tabs = Array.from(document.querySelectorAll('.nav-tab'));
    const currentIndex = tabs.indexOf(document.activeElement);
    let nextIndex;

    switch (event.key) {
        case 'ArrowRight':
            nextIndex = (currentIndex + 1) % tabs.length;
            break;
        case 'ArrowLeft':
            nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
            break;
        case 'Home':
            nextIndex = 0;
            break;
        case 'End':
            nextIndex = tabs.length - 1;
            break;
        default:
            return;
    }

    event.preventDefault();
    tabs[nextIndex].focus();
    tabs[nextIndex].click();
}

function loadSettings() {
    fetch('/api/settings')
    .then(response => response.json())
    .then(data => {
        document.getElementById('device-name').value = data.deviceName || '';
        document.getElementById('current-ssid').value = data.currentSSID || 'Non connecte';
    })
    .catch(error => {
        console.error('Error loading settings:', error);
    });
}

function refreshSettings() {
    loadSettings();
}

async function factoryReset() {
    if (!window.confirm('Effacer toutes les données persistantes, y compris l\'identité de l\'appareil, et redémarrer ? Cette opération est irréversible.')) {
        return;
    }
    const button = document.getElementById('factory-reset-btn');
    const status = document.getElementById('factory-reset-status');
    button.disabled = true;
    status.className = 'status info';
    status.textContent = 'Demande de réinitialisation en cours...';
    try {
        const response = await fetch('/api/system/factory-reset', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({confirm: true})
        });
        if (!response.ok) {
            throw new Error(await response.text());
        }
        status.textContent = 'Réinitialisation demandée. L\'enceinte va redémarrer.';
    } catch (error) {
        status.className = 'status error';
        status.textContent = 'Échec de la demande : ' + error.message;
        button.disabled = false;
    }
}

function loadFirmwareInfo() {
    fetch('/api/firmware/info')
    .then(response => response.json())
    .then(data => {
        document.getElementById('current-version').textContent = data.version || 'Inconnu';
        document.getElementById('current-date').textContent = data.date || 'Inconnu';
    })
    .catch(error => {
        console.error('Error loading firmware info:', error);
        document.getElementById('current-version').textContent = 'Erreur';
        document.getElementById('current-date').textContent = 'Erreur';
    });
}

function checkUpdate() {
    const checkBtn = document.getElementById('check-update-btn');
    const statusDiv = document.getElementById('upload-status');
    
    if (checkBtn) {
        checkBtn.disabled = true;
    }
    
    showStatus('Vérification des mises à jour...', 'info');
    
    fetch('/api/firmware/check')
    .then(response => response.json())
    .then(data => {
        if (checkBtn) {
            checkBtn.disabled = false;
        }
        
        if (data.updateAvailable) {
            showStatus('Mise à jour disponible! Version: ' + data.latestVersion, 'success');
            document.getElementById('update-btn').disabled = false;
        } else {
            showStatus('Aucune mise à jour disponible. Version actuelle: ' + data.currentVersion, 'info');
        }
    })
    .catch(error => {
        if (checkBtn) {
            checkBtn.disabled = false;
        }
        showStatus('Erreur: ' + error.message, 'error');
    });
}

// Initialize WebSocket connection for firmware update progress
function initWebSocket() {
    // Close existing connection if any
    if (firmwareUpdateState.ws) {
        firmwareUpdateState.ws.close();
        firmwareUpdateState.ws = null;
    }

    // Create new WebSocket connection
    // Use wss:// if the page was loaded with https://, otherwise ws://
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = protocol + '//' + window.location.host + '/ws';
    
    try {
        firmwareUpdateState.ws = new WebSocket(wsUrl);
        
        firmwareUpdateState.ws.onopen = function() {
            console.log('WebSocket connection established for firmware updates');
        };
        
        firmwareUpdateState.ws.onmessage = function(event) {
            const data = JSON.parse(event.data);
            console.log('WebSocket message:', data);
            
            if (data.type === 'firmware_progress') {
                handleFirmwareProgress(data.payload);
            } else if (data.type === 'firmware_update') {
                if (data.payload.success === true) {
                    handleFirmwareComplete(data.payload);
                } else if (data.payload.error) {
                    handleFirmwareFailed(data.payload);
                } else {
                    handleFirmwareUpdate(data.payload);
                }
            } else if (data.type === 'firmware_status') {
                handleFirmwareStatus(data.payload);
            }
        };
        
        firmwareUpdateState.ws.onclose = function() {
            console.log('WebSocket connection closed');
            // Attempt to reconnect after a delay
            setTimeout(initWebSocket, 5000);
        };
        
        firmwareUpdateState.ws.onerror = function(error) {
            console.error('WebSocket error:', error);
        };
        
    } catch (error) {
        console.error('Failed to create WebSocket:', error);
    }
}

// Handle firmware progress updates
function handleFirmwareProgress(progress) {
    firmwareUpdateState.currentProgress = progress;
    firmwareUpdateState.currentStatus = progress.status;
    
    // Update progress bar
    const progressContainer = document.getElementById('firmware-progress-container');
    if (progressContainer) {
        progressContainer.classList.remove('hidden');
    }
    const progressBar = document.getElementById('firmware-progress-bar');
    const progressText = document.getElementById('firmware-progress-text');
    const progressPercent = document.getElementById('firmware-progress-percent');
    
    if (progressBar) {
        progressBar.value = progress.step_percent || progress.download_percent || 0;
    }
    
    if (progressText) {
        let text = progress.message || 'Mise à jour en cours...';
        if (progress.total_steps > 0) {
            text = 'Étape ' + progress.current_step + '/' + progress.total_steps + ' - ' + text;
        }
        progressText.textContent = text;
    }
    
    if (progressPercent) {
        const percent = progress.step_percent || progress.download_percent || 0;
        progressPercent.textContent = percent + '%';
    }
    
    // Update status
    showStatus('Mise à jour en cours: ' + (progress.message || progress.status) + ' (' + 
        (progress.step_percent || progress.download_percent || 0) + '%)', 'info');
}

// Handle firmware update status
function handleFirmwareStatus(status) {
    firmwareUpdateState.currentStatus = status.status;
    
    if (status.status === 'idle') {
        firmwareUpdateState.uploadInProgress = false;
    } else if (status.status === 'start' || status.status === 'run' || status.status === 'download') {
        firmwareUpdateState.uploadInProgress = true;
    }
}

// Show the reboot landing page, which polls until the speaker is back
function redirectToRebooting() {
    if (firmwareUpdateState.rebooting) {
        return;
    }
    firmwareUpdateState.rebooting = true;
    window.location.href = '/rebooting';
}

// Handle firmware update completion
function handleFirmwareComplete(payload) {
    if (payload.reboot) {
        redirectToRebooting();
        return;
    }
    firmwareUpdateState.uploadInProgress = false;
    firmwareUpdateState.currentProgress = 100;
    firmwareUpdateState.currentStatus = 'success';
    
    showStatus('Mise à jour terminée avec succès! ' + payload.message, 'success');
    
    // Reset file input
    const fileInput = document.getElementById('firmware-file');
    if (fileInput) {
        fileInput.value = '';
    }
    
    // Re-enable buttons
    const updateBtn = document.getElementById('update-btn');
    const checkBtn = document.getElementById('check-update-btn');
    if (updateBtn) {
        updateBtn.disabled = false;
    }
    if (checkBtn) {
        checkBtn.disabled = false;
    }
    
    // Refresh firmware info
    setTimeout(loadFirmwareInfo, 2000);
}

// Handle firmware update failure
function handleFirmwareFailed(payload) {
    firmwareUpdateState.uploadInProgress = false;
    firmwareUpdateState.currentStatus = 'failed';
    firmwareUpdateState.error = payload.message || payload.error || 'Erreur inconnue';
    
    showStatus('Échec de la mise à jour: ' + firmwareUpdateState.error, 'error');
    
    // Re-enable buttons
    const updateBtn = document.getElementById('update-btn');
    const checkBtn = document.getElementById('check-update-btn');
    if (updateBtn) {
        updateBtn.disabled = false;
    }
    if (checkBtn) {
        checkBtn.disabled = false;
    }
}

// Handle firmware update started/stopped
function handleFirmwareUpdate(payload) {
    if (payload.status === 'started') {
        firmwareUpdateState.uploadInProgress = true;
        firmwareUpdateState.currentStatus = 'started';
        showStatus('Début de la mise à jour: ' + payload.message, 'info');
    }
}

// Show status message
function showStatus(message, type) {
    const statusDiv = document.getElementById('upload-status');
    if (statusDiv) {
        statusDiv.textContent = message;
        statusDiv.className = 'status ' + type;
        statusDiv.classList.remove('hidden');
    }
}

// Update firmware upload button state based on file selection
function updateUploadButtonState() {
    const fileInput = document.getElementById('firmware-file');
    const updateBtn = document.getElementById('update-btn');
    
    if (fileInput && updateBtn) {
        // Enable update button only if a file is selected and no update is in progress
        updateBtn.disabled = fileInput.files.length === 0 || firmwareUpdateState.uploadInProgress;
    }
}

// Update firmware function - handles file upload
async function updateFirmware() {
    const fileInput = document.getElementById('firmware-file');
    const file = fileInput.files[0];
    
    if (!file) {
        showStatus('Veuillez sélectionner un fichier de firmware', 'error');
        return;
    }
    
    // Validate file type
    const validTypes = ['application/octet-stream', 'application/bin', 'application/x-bin', ''];
    const validExtensions = ['.swu', '.bin', '.img'];
    const fileName = file.name.toLowerCase();
    const hasValidExtension = validExtensions.some(ext => fileName.endsWith(ext));
    
    if (!hasValidExtension) {
        showStatus('Type de fichier invalide. Veuillez sélectionner un fichier .swu, .bin ou .img', 'error');
        return;
    }
    
    // Check file size
    if (file.size > FIRMWARE_CONFIG.maxFileSize) {
        showStatus('Le fichier est trop grand. Taille maximale: ' + FIRMWARE_CONFIG.maxFileSizeDisplay, 'error');
        return;
    }
    
    // Confirm update
    if (!window.confirm('Êtes-vous sûr de vouloir installer ce firmware? Cette opération peut prendre plusieurs minutes.')) {
        return;
    }
    
    const updateBtn = document.getElementById('update-btn');
    const checkBtn = document.getElementById('check-update-btn');
    const statusDiv = document.getElementById('upload-status');
    
    // Set state
    firmwareUpdateState.uploadInProgress = true;
    firmwareUpdateState.currentProgress = 0;
    firmwareUpdateState.currentStatus = 'uploading';
    firmwareUpdateState.error = null;
    
    // Disable buttons
    if (updateBtn) updateBtn.disabled = true;
    if (checkBtn) checkBtn.disabled = true;
    
    // Show uploading status
    showStatus('Téléchargement du firmware: ' + file.name + ' (' + formatFileSize(file.size) + ')...', 'info');
    
    // Create FormData for file upload
    const formData = new FormData();
    formData.append('firmware', file);
    
    try {
        // Upload firmware file
        const response = await fetch('/api/firmware/upload', {
            method: 'POST',
            body: formData
        });
        
        if (!response.ok) {
            const errorData = await response.json().catch(() => ({}));
            throw new Error(errorData.message || errorData.error || 'Échec du téléchargement: ' + response.statusText);
        }
        
        const data = await response.json();
        
        if (data.status === 'started') {
            showStatus('Installation du firmware en cours...', 'info');
            // The update is now running in the background
            // Progress will be reported via WebSocket
        } else {
            throw new Error('Réponse inattendue du serveur');
        }
        
    } catch (error) {
        firmwareUpdateState.uploadInProgress = false;
        firmwareUpdateState.error = error.message;
        
        // Re-enable buttons
        if (updateBtn) updateBtn.disabled = false;
        if (checkBtn) checkBtn.disabled = false;
        
        showStatus('Erreur: ' + error.message, 'error');
        console.error('Firmware upload error:', error);
    }
}

// Cancel firmware update
async function cancelFirmwareUpdate() {
    if (!firmwareUpdateState.uploadInProgress) {
        return;
    }
    
    if (!window.confirm('Êtes-vous sûr de vouloir annuler la mise à jour du firmware?')) {
        return;
    }
    
    try {
        const response = await fetch('/api/firmware/cancel', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'}
        });
        
        if (response.ok) {
            showStatus('Mise à jour annulée', 'info');
            firmwareUpdateState.uploadInProgress = false;
            firmwareUpdateState.currentStatus = 'cancelled';
        } else {
            const errorData = await response.json().catch(() => ({}));
            showStatus('Échec de l\'annulation: ' + (errorData.error || response.statusText), 'error');
        }
    } catch (error) {
        showStatus('Erreur lors de l\'annulation: ' + error.message, 'error');
    }
}

// Get firmware update status
function getFirmwareStatus() {
    fetch('/api/firmware/status')
    .then(response => response.json())
    .then(data => {
        console.log('Firmware status:', data);
        const wasInProgress = firmwareUpdateState.uploadInProgress;
        firmwareUpdateState.currentStatus = data.status;
        firmwareUpdateState.uploadInProgress = data.is_running || data.in_progress;

        // Fallback if the completion event was missed on the WebSocket
        if (wasInProgress && data.status === 'success' && !data.is_running) {
            redirectToRebooting();
            return;
        }
        
        if (data.is_running || data.in_progress) {
            // Update is in progress, show status
            const progress = data.progress || {};
            const message = progress.message || 'Mise à jour en cours...';
            const percent = progress.step_percent || progress.download_percent || 0;
            showStatus(message + ' (' + percent + '%)', 'info');
        }
    })
    .catch(error => {
        console.error('Error getting firmware status:', error);
    });
}

// Format file size for display
function formatFileSize(bytes) {
    if (bytes < 1024) return bytes + ' octets';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' Ko';
    if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(2) + ' Mo';
    return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' Go';
}

// Set up file upload drag and drop
function setupFileUpload() {
    const fileUploadDiv = document.getElementById('file-upload');
    const fileInput = document.getElementById('firmware-file');
    
    if (!fileUploadDiv || !fileInput) {
        return;
    }
    
    // Handle file selection
    fileInput.addEventListener('change', function() {
        updateUploadButtonState();
        
        // Show selected file name
        if (this.files.length > 0) {
            const fileName = this.files[0].name;
            const fileInfo = document.getElementById('file-info');
            if (fileInfo) {
                fileInfo.textContent = 'Fichier sélectionné: ' + fileName + ' (' + formatFileSize(this.files[0].size) + ')';
                fileInfo.classList.remove('hidden');
            }
        }
    });
    
    // Drag and drop handlers
    fileUploadDiv.addEventListener('dragover', function(e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.add('dragover');
    });
    
    fileUploadDiv.addEventListener('dragleave', function(e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.remove('dragover');
    });
    
    fileUploadDiv.addEventListener('drop', function(e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.remove('dragover');
        
        if (e.dataTransfer.files.length > 0) {
            fileInput.files = e.dataTransfer.files;
            updateUploadButtonState();
            
            const fileName = e.dataTransfer.files[0].name;
            const fileInfo = document.getElementById('file-info');
            if (fileInfo) {
                fileInfo.textContent = 'Fichier sélectionné: ' + fileName + ' (' + formatFileSize(e.dataTransfer.files[0].size) + ')';
                fileInfo.classList.remove('hidden');
            }
        }
    });
    
    // Click to select file
    fileUploadDiv.addEventListener('click', function() {
        fileInput.click();
    });
}

function loadSystemInfo() {
    fetch('/api/system/info')
    .then(response => response.json())
    .then(data => {
        document.getElementById('system-mode').textContent = data.mode || 'Inconnu';
        document.getElementById('system-state').textContent = data.state || 'Inconnu';
        document.getElementById('ip-address').textContent = data.ipAddress || 'Inconnu';
        document.getElementById('network-ssid').textContent = data.ssid || 'Inconnu';
        document.getElementById('configure-wifi').hidden = data.mode !== 'FIRSTBOOT_AP';
    })
    .catch(error => {
        console.error('Error loading system info:', error);
    });
}

function refreshInfo() {
    loadSystemInfo();
}

// Initialize - load data on page load
document.addEventListener('DOMContentLoaded', function() {
    // Load initial data based on current tab
    const activeTab = document.querySelector('.tab-content.active');
    if (activeTab) {
        const tabId = activeTab.id;
        if (tabId === 'info') {
            loadSystemInfo();
        } else if (tabId === 'settings') {
            loadSettings();
        } else if (tabId === 'firmware') {
            loadFirmwareInfo();
        }
    }
    
    // Set up file upload
    setupFileUpload();
    
    // Initialize WebSocket connection
    initWebSocket();
    
    // Check firmware update status periodically
    setInterval(getFirmwareStatus, 5000);
    
    // Check if there's an update in progress on page load
    getFirmwareStatus();
});
