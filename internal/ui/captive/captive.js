let selectedNetwork = null;

function selectWifi(network, element) {
    document.querySelectorAll('.wifi-item').forEach(item => {
        item.classList.remove('selected');
    });
    element.classList.add('selected');
    selectedNetwork = network;
    document.getElementById('custom-ssid').value = '';
    document.getElementById('custom-password').value = '';
    updatePasswordField();
}

function selectCustomWifi() {
    if (selectedNetwork) {
        document.getElementById('custom-password').value = '';
    }
    selectedNetwork = null;
    document.querySelectorAll('.wifi-item').forEach(item => item.classList.remove('selected'));
    updatePasswordField();
}

function updatePasswordField() {
    const secured = selectedNetwork ? selectedNetwork.security === 'wpa-psk' : document.getElementById('custom-secured').checked;
    document.getElementById('password-group').classList.toggle('hidden', !secured);
    if (!secured) {
        document.getElementById('custom-password').value = '';
    }
}

function togglePassword() {
    document.getElementById('custom-password').type = document.getElementById('show-password').checked ? 'text' : 'password';
}

function connect() {
    const ssid = selectedNetwork ? selectedNetwork.ssid : document.getElementById('custom-ssid').value;
    const secured = selectedNetwork ? selectedNetwork.security === 'wpa-psk' : document.getElementById('custom-secured').checked;
    const password = secured ? document.getElementById('custom-password').value : '';
    
    if (!ssid) {
        showError('Veuillez entrer un nom de reseau');
        return;
    }
    
    if (secured && !password) {
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
            psk: password,
            hidden: !selectedNetwork
        })
    })
    .then(async response => {
        if (!response.ok) {
            throw new Error(await response.text());
        }
        return response.json();
    })
    .then(() => {
        document.getElementById('custom-password').value = '';
        showSuccess('Configuration enregistree. Connexion au reseau en cours...');
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
            throw new Error('Recherche Wi-Fi impossible. Reessayez ou saisissez le reseau manuellement.');
        }
        return response.json();
    })
    .then(data => {
        selectCustomWifi();
        document.getElementById('custom-password').value = '';
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
    if (networks.length === 0) {
        wifiList.textContent = 'Aucun reseau visible. Vous pouvez saisir un reseau manuellement.';
    }
    
    networks.forEach(network => {
        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'wifi-item';
        item.dataset.ssid = network.ssid;
        item.disabled = network.security === 'unsupported';
        item.onclick = () => selectWifi(network, item);
        
        item.innerHTML = `
            <svg class="wifi-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 2C10.5 2 9.2 2.8 8.3 4H5.7C4.8 2.8 3.5 2 2 2M22 2c-1.5 0-2.8.8-3.7 2H16.3c-.9-1.2-2.2-2-3.7-2M5 8c-1.1 0-2 .9-2 2s.9 2 2 2M19 8c1.1 0 2 .9 2 2s-.9 2-2 2M2 14c-1.1 0-2 .9-2 2s.9 2 2 2M22 14c1.1 0 2 .9 2 2s-.9 2-2 2M12 22c-1.1 0-2-.9-2-2s.9-2 2-2"/>
            </svg>
            <div class="wifi-info">
                <div class="wifi-name"></div>
                <div class="wifi-ssid"></div>
            </div>
            <div class="signal-strength">
                <div class="signal-fill"></div>
            </div>
            <div class="signal-text"></div>
        `;
        const signal = Math.max(0, Math.min(100, Number(network.signalPercent) || 0));
        item.querySelector('.wifi-name').textContent = network.ssid;
        item.querySelector('.wifi-ssid').textContent = network.security === 'open' ? 'Ouvert' :
            network.security === 'wpa-psk' ? 'WPA/WPA2 - mot de passe requis' : 'Securite non prise en charge';
        item.querySelector('.signal-fill').style.width = signal + '%';
        item.querySelector('.signal-text').textContent = signal + '%';
        
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
    updatePasswordField();
    refresh();
});
