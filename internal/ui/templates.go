package ui

// HTML templates for the embedded web interfaces

// CaptivePortalTemplate is the HTML template for the captive portal
const CaptivePortalTemplate = `<!DOCTYPE html>
<html lang="fr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Speakerctld - Configuration WiFi</title>
    <style>
        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 500px;
            margin: 0 auto;
            background: white;
            border-radius: 12px;
            box-shadow: 0 10px 40px rgba(0,0,0,0.2);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 30px;
            text-align: center;
        }
        .header h1 {
            font-size: 24px;
            margin-bottom: 10px;
        }
        .header p {
            opacity: 0.9;
            font-size: 14px;
        }
        .content {
            padding: 30px;
        }
        .wifi-list {
            margin: 20px 0;
        }
        .wifi-item {
            display: flex;
            align-items: center;
            padding: 15px;
            border: 2px solid #e0e0e0;
            border-radius: 8px;
            margin: 10px 0;
            cursor: pointer;
            transition: all 0.2s;
        }
        .wifi-item:hover {
            border-color: #667eea;
            background: #f8f9ff;
        }
        .wifi-item.selected {
            border-color: #667eea;
            background: #e8eaff;
        }
        .wifi-icon {
            width: 24px;
            height: 24px;
            margin-right: 15px;
        }
        .signal-strength {
            flex: 1;
            height: 8px;
            background: #e0e0e0;
            border-radius: 4px;
            margin: 0 15px;
            position: relative;
        }
        .signal-fill {
            height: 100%;
            background: #4CAF50;
            border-radius: 4px;
        }
        .wifi-info {
            flex: 1;
        }
        .wifi-name {
            font-weight: 600;
            font-size: 16px;
        }
        .wifi-ssid {
            color: #666;
            font-size: 12px;
        }
        .signal-text {
            width: 60px;
            text-align: right;
            font-size: 12px;
            color: #666;
        }
        .form-group {
            margin: 20px 0;
        }
        .form-group label {
            display: block;
            margin-bottom: 8px;
            font-weight: 600;
            color: #333;
        }
        .form-group input {
            width: 100%;
            padding: 12px;
            border: 2px solid #e0e0e0;
            border-radius: 8px;
            font-size: 16px;
            transition: border-color 0.2s;
        }
        .form-group input:focus {
            outline: none;
            border-color: #667eea;
        }
        .btn {
            display: inline-block;
            padding: 12px 30px;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            border-radius: 8px;
            font-size: 16px;
            font-weight: 600;
            cursor: pointer;
            transition: transform 0.2s, box-shadow 0.2s;
            margin: 5px;
        }
        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 5px 20px rgba(102, 126, 234, 0.4);
        }
        .btn:active {
            transform: translateY(0);
        }
        .btn-secondary {
            background: #e0e0e0;
            color: #333;
        }
        .btn-secondary:hover {
            background: #d0d0d0;
            box-shadow: 0 5px 20px rgba(0,0,0,0.2);
        }
        .buttons {
            display: flex;
            justify-content: center;
            margin: 30px 0;
            gap: 10px;
        }
        .custom-network {
            margin-top: 30px;
            padding: 20px;
            border: 2px dashed #e0e0e0;
            border-radius: 8px;
            text-align: center;
        }
        .custom-network h3 {
            margin-bottom: 15px;
            color: #667eea;
        }
        .custom-network p {
            color: #666;
            margin-bottom: 20px;
        }
        .loading {
            text-align: center;
            padding: 20px;
            color: #667eea;
        }
        .spinner {
            border: 3px solid #f3f3f3;
            border-top: 3px solid #667eea;
            border-radius: 50%;
            width: 30px;
            height: 30px;
            animation: spin 1s linear infinite;
            margin: 20px auto;
        }
        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }
        .error {
            color: #e74c3c;
            text-align: center;
            padding: 10px;
            background: #fdecea;
            border-radius: 4px;
            margin: 20px 0;
        }
        .success {
            color: #27ae60;
            text-align: center;
            padding: 10px;
            background: #e8f8f0;
            border-radius: 4px;
            margin: 20px 0;
        }
        .hidden {
            display: none;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Configuration WiFi Speakerctld</h1>
            <p>Connectez votre enceinte au reseau WiFi</p>
        </div>
        <div class="content">
            <div id="loading" class="loading">
                <div class="spinner"></div>
                <p>Recherche des reseaux WiFi...</p>
            </div>
            
            <div id="error" class="error hidden"></div>
            <div id="success" class="success hidden"></div>
            
            <h2>Reseaux disponibles</h2>
            <div id="wifi-list" class="wifi-list">
                <div class="wifi-item" data-ssid="Speakerctld-Setup-ABCD" onclick="selectWifi('Speakerctld-Setup-ABCD', this)">
                    <svg class="wifi-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M12 2C10.5 2 9.2 2.8 8.3 4H5.7C4.8 2.8 3.5 2 2 2M22 2c-1.5 0-2.8.8-3.7 2H16.3c-.9-1.2-2.2-2-3.7-2M5 8c-1.1 0-2 .9-2 2s.9 2 2 2M19 8c1.1 0 2 .9 2 2s-.9 2-2 2M2 14c-1.1 0-2 .9-2 2s.9 2 2 2M22 14c1.1 0 2 .9 2 2s-.9 2-2 2M12 22c-1.1 0-2-.9-2-2s.9-2 2-2"/>
                    </svg>
                    <div class="wifi-info">
                        <div class="wifi-name">Speakerctld-Setup-ABCD</div>
                    </div>
                    <div class="signal-strength">
                        <div class="signal-fill" style="width: 100%"></div>
                    </div>
                    <div class="signal-text">100%</div>
                </div>
            </div>
            
            <div class="custom-network">
                <h3>ou</h3>
                <p>Connectez-vous a un reseau cache ou personnalise</p>
                <div class="form-group">
                    <label for="custom-ssid">Nom du reseau (SSID)</label>
                    <input type="text" id="custom-ssid" placeholder="Nom du reseau">
                </div>
                <div class="form-group">
                    <label for="custom-password">Mot de passe</label>
                    <input type="password" id="custom-password" placeholder="Mot de passe">
                </div>
            </div>
            
            <div class="buttons">
                <button class="btn" onclick="connect()">Se connecter</button>
                <button class="btn btn-secondary" onclick="refresh()">Actualiser</button>
            </div>
        </div>
    </div>
    
    <script>
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
                location.reload();
            })
            .catch(error => {
                showError('Erreur: ' + error.message);
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
    </script>
</body>
</html>`

// SettingsTemplate is the HTML template for the settings/firmware update interface
const SettingsTemplate = `<!DOCTYPE html>
<html lang="fr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Speakerctld - Parametres</title>
    <style>
        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: #f5f7fa;
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 800px;
            margin: 0 auto;
            background: white;
            border-radius: 12px;
            box-shadow: 0 2px 20px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 30px;
            text-align: center;
        }
        .header h1 {
            font-size: 24px;
            margin-bottom: 10px;
        }
        .content {
            padding: 30px;
        }
        .nav-tabs {
            display: flex;
            border-bottom: 2px solid #e0e0e0;
            margin-bottom: 30px;
        }
        .nav-tab {
            flex: 1;
            padding: 15px;
            text-align: center;
            cursor: pointer;
            font-weight: 600;
            color: #666;
            transition: all 0.2s;
            border-bottom: 3px solid transparent;
        }
        .nav-tab:hover {
            color: #667eea;
        }
        .nav-tab.active {
            color: #667eea;
            border-bottom-color: #667eea;
        }
        .tab-content {
            display: none;
        }
        .tab-content.active {
            display: block;
        }
        .settings-section {
            margin: 20px 0;
            padding: 20px;
            background: #f8f9ff;
            border-radius: 8px;
        }
        .settings-section h2 {
            margin-bottom: 15px;
            color: #333;
            font-size: 18px;
        }
        .form-group {
            margin: 15px 0;
        }
        .form-group label {
            display: block;
            margin-bottom: 8px;
            font-weight: 600;
            color: #333;
        }
        .form-group input,
        .form-group select {
            width: 100%;
            padding: 12px;
            border: 2px solid #e0e0e0;
            border-radius: 8px;
            font-size: 16px;
            transition: border-color 0.2s;
        }
        .form-group input:focus,
        .form-group select:focus {
            outline: none;
            border-color: #667eea;
        }
        .btn {
            display: inline-block;
            padding: 12px 30px;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            border-radius: 8px;
            font-size: 16px;
            font-weight: 600;
            cursor: pointer;
            transition: transform 0.2s, box-shadow 0.2s;
            margin: 5px;
        }
        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 5px 20px rgba(102, 126, 234, 0.4);
        }
        .btn:active {
            transform: translateY(0);
        }
        .btn-secondary {
            background: #e0e0e0;
            color: #333;
        }
        .btn-secondary:hover {
            background: #d0d0d0;
            box-shadow: 0 5px 20px rgba(0,0,0,0.2);
        }
        .btn-danger {
            background: #e74c3c;
        }
        .btn-danger:hover {
            background: #c0392b;
        }
        .card {
            background: white;
            border-radius: 8px;
            padding: 20px;
            margin: 20px 0;
            box-shadow: 0 2px 10px rgba(0,0,0,0.05);
        }
        .card h3 {
            margin-bottom: 15px;
            color: #333;
        }
        .firmware-info {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 10px 0;
            border-bottom: 1px solid #e0e0e0;
        }
        .firmware-info:last-child {
            border-bottom: none;
        }
        .firmware-label {
            color: #666;
            font-weight: 600;
        }
        .firmware-value {
            color: #333;
        }
        .file-upload {
            border: 2px dashed #e0e0e0;
            border-radius: 8px;
            padding: 30px;
            text-align: center;
            cursor: pointer;
            transition: all 0.2s;
        }
        .file-upload:hover {
            border-color: #667eea;
            background: #f8f9ff;
        }
        .file-upload.dragover {
            border-color: #667eea;
            background: #e8eaff;
        }
        .file-upload input {
            display: none;
        }
        .file-upload label {
            cursor: pointer;
        }
        .file-upload svg {
            width: 48px;
            height: 48px;
            color: #667eea;
            margin-bottom: 10px;
        }
        .status {
            padding: 10px;
            border-radius: 4px;
            margin: 10px 0;
            text-align: center;
        }
        .status.success {
            background: #e8f8f0;
            color: #27ae60;
        }
        .status.error {
            background: #fdecea;
            color: #e74c3c;
        }
        .status.info {
            background: #e8f4fd;
            color: #2980b9;
        }
        .system-info {
            font-size: 12px;
            color: #666;
            margin-top: 20px;
            padding-top: 20px;
            border-top: 1px solid #e0e0e0;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Parametres Speakerctld</h1>
        </div>
        <div class="content">
            <div class="nav-tabs">
                <div class="nav-tab active" onclick="switchTab(event, 'settings')">Parametres</div>
                <div class="nav-tab" onclick="switchTab(event, 'firmware')">Mise a jour firmware</div>
                <div class="nav-tab" onclick="switchTab(event, 'info')">Informations</div>
            </div>
            
            <div id="settings" class="tab-content active">
                <h2>Parametres systeme</h2>
                <p style="color: #666; margin-bottom: 20px;">Les parametres sont temporairement desactivees. Cette fonctionnalite sera disponible dans une future mise a jour.</p>
                
                <div class="settings-section">
                    <h2>Parametres reseau</h2>
                    <div class="form-group">
                        <label for="device-name">Nom de l'appareil</label>
                        <input type="text" id="device-name" placeholder="Nom de l'appareil" readonly>
                    </div>
                    <div class="form-group">
                        <label for="current-ssid">Reseau WiFi actuel</label>
                        <input type="text" id="current-ssid" placeholder="Non connecte" readonly>
                    </div>
                </div>
                
                <div class="buttons">
                    <button class="btn btn-secondary" onclick="refreshSettings()">Actualiser</button>
                </div>
            </div>
            
            <div id="firmware" class="tab-content">
                <h2>Mise a jour du firmware</h2>
                <p style="color: #666; margin-bottom: 20px;">Telechargez un fichier de mise a jour du firmware pour mettre a jour votre enceinte Speakerctld.</p>
                
                <div class="card">
                    <h3>Firmware actuel</h3>
                    <div class="firmware-info">
                        <span class="firmware-label">Version:</span>
                        <span class="firmware-value" id="current-version">Chargement...</span>
                    </div>
                    <div class="firmware-info">
                        <span class="firmware-label">Date:</span>
                        <span class="firmware-value" id="current-date">Chargement...</span>
                    </div>
                </div>
                
                <div class="card">
                    <h3>Telecharger une mise a jour</h3>
                    <p style="color: #666; margin-bottom: 15px;">Fonctionnalite temporairement desactivee.</p>
                    <div class="file-upload" id="file-upload">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
                            <polyline points="17 8 12 3 7 8"/>
                            <line x1="12" y1="3" x2="12" y2="15"/>
                        </svg>
                        <p>Glissez-deposez un fichier ou cliquez pour selectionner</p>
                        <input type="file" id="firmware-file" accept=".bin,.img">
                    </div>
                    <div id="upload-status" class="status hidden"></div>
                </div>
                
                <div class="buttons">
                    <button class="btn" onclick="updateFirmware()" id="update-btn" disabled>Mettre a jour</button>
                    <button class="btn btn-secondary" onclick="checkUpdate()">Verifier les mises a jour</button>
                </div>
            </div>
            
            <div id="info" class="tab-content">
                <h2>Informations systeme</h2>
                
                <div class="card">
                    <h3>Systeme</h3>
                    <div class="firmware-info">
                        <span class="firmware-label">Nom:</span>
                        <span class="firmware-value">Speakerctld Speaker</span>
                    </div>
                    <div class="firmware-info">
                        <span class="firmware-label">Mode:</span>
                        <span class="firmware-value" id="system-mode">Chargement...</span>
                    </div>
                    <div class="firmware-info">
                        <span class="firmware-label">Etat:</span>
                        <span class="firmware-value" id="system-state">Chargement...</span>
                    </div>
                </div>
                
                <div class="card">
                    <h3>Reseau</h3>
                    <div class="firmware-info">
                        <span class="firmware-label">Adresse IP:</span>
                        <span class="firmware-value" id="ip-address">Chargement...</span>
                    </div>
                    <div class="firmware-info">
                        <span class="firmware-label">SSID:</span>
                        <span class="firmware-value" id="network-ssid">Chargement...</span>
                    </div>
                </div>
                
                <div class="buttons">
                    <button class="btn btn-secondary" onclick="refreshInfo()">Actualiser</button>
                </div>
            </div>
        </div>
    </div>
    
    <script>
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
        
        loadSettings();
        loadSystemInfo();
    </script>
</body>
</html>`

// WifiNetwork represents a WiFi network for the captive portal
type WifiNetwork struct {
	SSID           string
	SignalStrength int
	SignalPercent  int
}

// SettingsData represents settings data for the settings page
type SettingsData struct {
	DeviceName string
	CurrentSSID string
}

// FirmwareInfo represents firmware information
type FirmwareInfo struct {
	Version string
	Date    string
}

// SystemInfo represents system information
type SystemInfo struct {
	Mode      string
	State     string
	IPAddress string
	SSID      string
}
