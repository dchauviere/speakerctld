function switchTab(event, tabName) {
    document.querySelectorAll('.tab-content').forEach(tab => {
        tab.classList.remove('active');
    });
    document.querySelectorAll('.nav-tab').forEach(tab => {
        tab.classList.remove('active');
    });
    
    document.getElementById(tabName).classList.add('active');
    event.currentTarget.classList.add('active');
    
    if (tabName === 'settings') {
        loadSettings();
    } else if (tabName === 'firmware') {
        loadFirmwareInfo();
    } else if (tabName === 'info') {
        loadSystemInfo();
    }
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

function loadFirmwareInfo() {
    fetch('/api/firmware/info')
    .then(response => response.json())
    .then(data => {
        document.getElementById('current-version').textContent = data.version || 'Inconnu';
        document.getElementById('current-date').textContent = data.date || 'Inconnu';
    })
    .catch(error => {
        console.error('Error loading firmware info:', error);
    });
}

function checkUpdate() {
    showStatus('Verification des mises a jour...', 'info');
    fetch('/api/firmware/check')
    .then(response => response.json())
    .then(data => {
        if (data.updateAvailable) {
            showStatus('Mise a jour disponible!', 'success');
            document.getElementById('update-btn').disabled = false;
        } else {
            showStatus('Aucune mise a jour disponible.', 'info');
        }
    })
    .catch(error => {
        showStatus('Erreur: ' + error.message, 'error');
    });
}

function updateFirmware() {
    const fileInput = document.getElementById('firmware-file');
    const file = fileInput.files[0];
    
    if (!file) {
        showStatus('Veuillez selectionner un fichier', 'error');
        return;
    }
    
    showStatus('Telechargement en cours...', 'info');
    showStatus('Mise a jour du firmware: Fonctionnalite temporairement desactivee', 'info');
}

function loadSystemInfo() {
    fetch('/api/system/info')
    .then(response => response.json())
    .then(data => {
        document.getElementById('system-mode').textContent = data.mode || 'Inconnu';
        document.getElementById('system-state').textContent = data.state || 'Inconnu';
        document.getElementById('ip-address').textContent = data.ipAddress || 'Inconnu';
        document.getElementById('network-ssid').textContent = data.ssid || 'Inconnu';
    })
    .catch(error => {
        console.error('Error loading system info:', error);
    });
}

function refreshInfo() {
    loadSystemInfo();
}

function showStatus(message, type) {
    const statusDiv = document.getElementById('upload-status');
    statusDiv.textContent = message;
    statusDiv.className = 'status ' + type;
    statusDiv.classList.remove('hidden');
}

// Initialize - load data on page load
document.addEventListener('DOMContentLoaded', function() {
    loadSettings();
    loadSystemInfo();
});
