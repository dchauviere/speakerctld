let selectedSSID = '';

function selectWifi(ssid, element) {
    document.querySelectorAll('.wifi-item').forEach(item => {
        item.classList.remove('selected');
    });
    element.classList.add('selected');
    selectedSSID = ssid;
    document.getElementById('custom-ssid').value = '';
    document.getElementById('custom-password').value = '';
}

function connect() {
    let ssid, password;
    
    if (selectedSSID) {
        ssid = selectedSSID;
        password = document.getElementById('custom-password').value;
    } else {
        ssid = document.getElementById('custom-ssid').value;
        password = document.getElementById('custom-password').value;
    }
    
    if (!ssid) {
        showError('Veuillez entrer un nom de reseau');
        return;
    }
    
    if (!password) {
        showError('Veuillez entrer un mot de passe');
        return;
    }
    
    showLoading('Connexion en cours...');
    
    fetch('/api/wifi', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({
            ssid: ssid,
            psk: password
        })
    })
    .then(response => {
        if (!response.ok) {
            throw new Error('Erreur de connexion');
        }
        return response.json();
    })
    .then(data => {
        showSuccess('Connexion reussie! L\'appareil va redemarrer.');
        setTimeout(() => {
            showLoading('Redemarrage en cours...');
        }, 2000);
    })
    .catch(error => {
        showError('Erreur: ' + error.message);
    });
}

function refresh() {
    showLoading('Recherche des reseaux WiFi...');
    fetch('/api/wifi/scan')
    .then(response => {
        if (!response.ok) {
            throw new Error('Erreur lors du scan');
        }
        return response.json();
    })
    .then(data => {
        populateWifiList(data);
        hideLoading();
    })
    .catch(error => {
        showError('Erreur: ' + error.message);
    });
}

function populateWifiList(networks) {
    const wifiList = document.getElementById('wifi-list');
    wifiList.innerHTML = '';
    
    networks.forEach(network => {
        const item = document.createElement('div');
        item.className = 'wifi-item';
        item.dataset.ssid = network.SSID;
        item.onclick = () => selectWifi(network.SSID, item);
        
        item.innerHTML = `
            <svg class="wifi-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2C10.5 2 9.2 2.8 8.3 4H5.7C4.8 2.8 3.5 2 2 2M22 2c-1.5 0-2.8.8-3.7 2H16.3c-.9-1.2-2.2-2-3.7-2M5 8c-1.1 0-2 .9-2 2s.9 2 2 2M19 8c1.1 0 2 .9 2 2s-.9 2-2 2M2 14c-1.1 0-2 .9-2 2s.9 2 2 2M22 14c1.1 0 2 .9 2 2s-.9 2-2 2M12 22c-1.1 0-2-.9-2-2s.9-2 2-2"/>
            </svg>
            <div class="wifi-info">
                <div class="wifi-name">${network.SSID}</div>
            </div>
            <div class="signal-strength">
                <div class="signal-fill" style="width: ${network.SignalPercent}%"></div>
            </div>
            <div class="signal-text">${network.SignalPercent}%</div>
        `;
        
        wifiList.appendChild(item);
    });
}

function showLoading(message) {
    document.getElementById('loading').classList.remove('hidden');
    document.getElementById('error').classList.add('hidden');
    document.getElementById('success').classList.add('hidden');
    document.getElementById('loading').innerHTML = '<div class="spinner"></div><p>' + message + '</p>';
}

function hideLoading() {
    document.getElementById('loading').classList.add('hidden');
}

function showError(message) {
    hideLoading();
    document.getElementById('error').textContent = message;
    document.getElementById('error').classList.remove('hidden');
}

function showSuccess(message) {
    hideLoading();
    document.getElementById('success').textContent = message;
    document.getElementById('success').classList.remove('hidden');
}

// Initialize - load WiFi networks on page load
document.addEventListener('DOMContentLoaded', function() {
    refresh();
});
