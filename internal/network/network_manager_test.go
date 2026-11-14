package network

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dchauviere/speakerctld/internal/storage"
	"github.com/godbus/dbus/v5"
)

type testNetworkManager struct {
	mu          sync.Mutex
	connections map[dbus.ObjectPath]string
	failDelete  bool
	apSettings  map[string]map[string]dbus.Variant
	apOptions   map[string]dbus.Variant
	apState     uint32
	deactivated bool
}

const testDevicePath = dbus.ObjectPath("/org/freedesktop/NetworkManager/Devices/1")
const testActivePath = dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/1")

func (m *testNetworkManager) GetDeviceByIpIface(iface string) (dbus.ObjectPath, *dbus.Error) {
	if iface != "wlan-test" {
		return "/", dbus.NewError("org.freedesktop.NetworkManager.UnknownDevice", nil)
	}
	return testDevicePath, nil
}

func (m *testNetworkManager) GetAllDevices() ([]dbus.ObjectPath, *dbus.Error) {
	return []dbus.ObjectPath{testDevicePath}, nil
}

func (m *testNetworkManager) AddAndActivateConnection2(settings map[string]map[string]dbus.Variant, device, specific dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, dbus.ObjectPath, map[string]dbus.Variant, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apSettings, m.apOptions = settings, options
	if device != testDevicePath || specific != "/" {
		return "/", "/", nil, dbus.NewError("org.freedesktop.NetworkManager.InvalidArguments", nil)
	}
	return "/org/freedesktop/NetworkManager/Settings/3", testActivePath, map[string]dbus.Variant{}, nil
}

func (m *testNetworkManager) DeactivateConnection(path dbus.ObjectPath) *dbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deactivated = path == testActivePath
	return nil
}

type testAPProperties struct{ manager *testNetworkManager }

func (p *testAPProperties) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	if iface == NMDeviceIface && property == "DeviceType" {
		return dbus.MakeVariant(NMDeviceTypeWifi), nil
	}
	if iface == activeConnectionInterface && property == "State" {
		p.manager.mu.Lock()
		defer p.manager.mu.Unlock()
		return dbus.MakeVariant(p.manager.apState), nil
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", nil)
}

func (m *testNetworkManager) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	if iface != NMInterface || property != "Version" {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", nil)
	}
	return dbus.MakeVariant("test"), nil
}

func (m *testNetworkManager) ListConnections() ([]dbus.ObjectPath, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	paths := make([]dbus.ObjectPath, 0, len(m.connections))
	for path := range m.connections {
		paths = append(paths, path)
	}
	return paths, nil
}

type testConnection struct {
	manager *testNetworkManager
	path    dbus.ObjectPath
}

func (c *testConnection) GetSettings() (map[string]map[string]dbus.Variant, *dbus.Error) {
	c.manager.mu.Lock()
	defer c.manager.mu.Unlock()
	id := c.manager.connections[c.path]
	return map[string]map[string]dbus.Variant{
		"connection": {"id": dbus.MakeVariant(id)},
	}, nil
}

func (c *testConnection) Delete() *dbus.Error {
	c.manager.mu.Lock()
	defer c.manager.mu.Unlock()
	if c.manager.failDelete {
		return dbus.NewError("org.freedesktop.NetworkManager.TestError", []interface{}{"delete failed"})
	}
	delete(c.manager.connections, c.path)
	return nil
}

func startTestNetworkManager(t *testing.T) (*testNetworkManager, dbus.ObjectPath, dbus.ObjectPath) {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is unavailable")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil && err != os.ErrProcessDone {
			t.Errorf("stop test bus: %v", err)
		}
		cmd.Wait()
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatalf("read test bus address: %v", scanner.Err())
	}
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", scanner.Text())

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	reply, err := conn.RequestName(NMService, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request NetworkManager name: reply=%v err=%v", reply, err)
	}
	speakerPath := dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings/1")
	otherPath := dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings/2")
	manager := &testNetworkManager{connections: map[dbus.ObjectPath]string{
		speakerPath: "Speaker-test",
		otherPath:   "Other-network",
	}}
	for _, export := range []struct {
		value interface{}
		path  dbus.ObjectPath
		iface string
	}{
		{manager, dbus.ObjectPath(NMPath), "org.freedesktop.DBus.Properties"},
		{manager, dbus.ObjectPath(NMPath), NMInterface},
		{manager, "/org/freedesktop/NetworkManager/Settings", NMSettingsIface},
		{&testAPProperties{manager}, testDevicePath, "org.freedesktop.DBus.Properties"},
		{&testAPProperties{manager}, testActivePath, "org.freedesktop.DBus.Properties"},
		{&testConnection{manager, speakerPath}, speakerPath, NMSettingsIface + ".Connection"},
		{&testConnection{manager, otherPath}, otherPath, NMSettingsIface + ".Connection"},
	} {
		if err := conn.Export(export.value, export.path, export.iface); err != nil {
			t.Fatal(err)
		}
	}
	return manager, speakerPath, otherPath
}

func TestStartAccessPoint(t *testing.T) {
	manager, _, _ := startTestNetworkManager(t)
	client, err := NewNetworkManagerClient()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	for _, tc := range []struct {
		name, password, address string
		state                   uint32
		wantErr, rollback       bool
	}{
		{"first boot", "", FirstBootAPIP, 2, false, false},
		{"fallback", "recovery-password", FallbackAPIP, 2, false, false},
		{"activation failed", "", FirstBootAPIP, 4, true, true},
		{"activation timed out", "", FirstBootAPIP, 1, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager.mu.Lock()
			manager.apState, manager.deactivated = tc.state, false
			manager.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			err := client.StartAccessPoint(ctx, "wlan-test", "Speakerctld-Test", tc.password, tc.address)
			if (err != nil) != tc.wantErr {
				t.Fatalf("StartAccessPoint error = %v", err)
			}
			manager.mu.Lock()
			defer manager.mu.Unlock()
			if manager.deactivated != tc.rollback {
				t.Fatalf("deactivated = %v, want %v", manager.deactivated, tc.rollback)
			}
			settings := manager.apSettings
			if settings["connection"]["interface-name"].Value() != "wlan-test" ||
				settings["802-11-wireless"]["mode"].Value() != "ap" ||
				settings["ipv4"]["method"].Value() != "manual" ||
				manager.apOptions["persist"].Value() != "volatile" {
				t.Fatalf("unexpected settings: %v, options: %v", settings, manager.apOptions)
			}
			addresses, ok := settings["ipv4"]["address-data"].Value().([]map[string]dbus.Variant)
			if !ok || len(addresses) != 1 || addresses[0]["address"].Value() != tc.address || addresses[0]["prefix"].Value() != uint32(24) {
				t.Fatalf("unexpected address-data: %v", settings["ipv4"]["address-data"])
			}
			security, secured := settings["802-11-wireless-security"]
			if secured != (tc.password != "") || (secured && security["psk"].Value() != tc.password) {
				t.Fatalf("unexpected security settings: %v", security)
			}
		})
	}
	if err := client.StartAccessPoint(context.Background(), "missing", "test", "", FirstBootAPIP); err == nil {
		t.Fatal("expected an error for a missing interface")
	}
}

func TestWifiClientUsesVolatileProfile(t *testing.T) {
	manager, _, _ := startTestNetworkManager(t)
	client, err := NewNetworkManagerClient()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.AddWifiConnection("home", "test-password"); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.apOptions["persist"].Value() != "volatile" ||
		manager.apSettings["802-11-wireless"]["mode"].Value() != "infrastructure" ||
		manager.apSettings["802-11-wireless-security"]["psk"].Value() != "test-password" {
		t.Fatalf("unexpected client profile: %v, options: %v", manager.apSettings, manager.apOptions)
	}
	if _, ok := manager.apSettings["connection"]["volatile"]; ok {
		t.Fatal("volatile is an activation option, not a connection setting")
	}
}

func TestAccessPointAndDNSConfigurationAgree(t *testing.T) {
	manager, _, _ := startTestNetworkManager(t)
	manager.mu.Lock()
	manager.apState = 2
	manager.mu.Unlock()
	client, err := NewWifiConnectionManager()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "device"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "device", "recovery-password"), []byte("my-recovery-password"), 0600); err != nil {
		t.Fatal(err)
	}
	g := NewConfigGenerator(storage.NewFileStorage(dir), WithNetworkManagerClient(client), WithWifiInterface("wlan-test"))
	defer g.Close()
	g.runtimeDir = t.TempDir()
	g.dnsmasqTemplatePath = filepath.Join("..", "..", "etc", "dnsmasq.conf.gotmpl")
	for _, tc := range []struct {
		state                          State
		ssid, ip, start, end, password string
	}{
		{FirstBootAP, "Speakerctld-Setup-ABCD", FirstBootAPIP, FirstBootAPStart, FirstBootAPEnd, ""},
		{FallbackAP, "Speakerctld-Recovery-ABCD", FallbackAPIP, FallbackAPStart, FallbackAPEnd, "my-recovery-password"},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			if err := g.GenerateDnsmasqConfig(tc.state); err != nil {
				t.Fatal(err)
			}
			if err := g.ConfigureAccessPoint(tc.state, "ABCD"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(g.runtimeDir, "dnsmasq.conf"))
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{
				"interface=wlan-test",
				"dhcp-range=" + tc.start + "," + tc.end + ",255.255.255.0",
				"dhcp-option=3," + tc.ip,
				"dhcp-option=6," + tc.ip,
			} {
				if !strings.Contains(string(data), expected) {
					t.Errorf("dnsmasq config missing %q", expected)
				}
			}
			manager.mu.Lock()
			defer manager.mu.Unlock()
			settings := manager.apSettings
			ssid, ok := settings["802-11-wireless"]["ssid"].Value().([]byte)
			if !ok || string(ssid) != tc.ssid {
				t.Fatalf("unexpected AP SSID: %v", settings["802-11-wireless"]["ssid"])
			}
			addresses := settings["ipv4"]["address-data"].Value().([]map[string]dbus.Variant)
			if addresses[0]["address"].Value() != tc.ip {
				t.Fatalf("AP address does not match dnsmasq: %v", addresses)
			}
			security, secured := settings["802-11-wireless-security"]
			if secured != (tc.password != "") || (secured && security["psk"].Value() != tc.password) {
				t.Fatalf("unexpected AP security: %v", security)
			}
		})
	}
}

func TestNetworkManagerConnectionOwnershipAndCleanup(t *testing.T) {
	manager, speakerPath, otherPath := startTestNetworkManager(t)
	first, err := NewWifiConnectionManager()
	if err != nil {
		t.Fatal(err)
	}
	g := &ConfigGenerator{runtimeDir: t.TempDir(), nmManager: first}
	t.Cleanup(g.Close)

	second, err := NewWifiConnectionManager()
	if err != nil {
		t.Fatal(err)
	}
	second.Close()

	for _, name := range []string{"hostapd.conf", "dnsmasq.conf"} {
		if err := os.WriteFile(filepath.Join(g.runtimeDir, name), []byte("active AP"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	manager.mu.Lock()
	manager.failDelete = true
	manager.mu.Unlock()
	if err := g.CleanupWifiConnections(); err == nil || !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("expected deletion error, got %v", err)
	}
	manager.mu.Lock()
	manager.failDelete = false
	manager.mu.Unlock()
	if err := g.CleanupWifiConnections(); err != nil {
		t.Fatalf("retry Wi-Fi cleanup: %v", err)
	}
	manager.mu.Lock()
	_, speakerExists := manager.connections[speakerPath]
	_, otherExists := manager.connections[otherPath]
	manager.mu.Unlock()
	if speakerExists || !otherExists {
		t.Fatalf("cleanup removed wrong connections: speaker=%v other=%v", speakerExists, otherExists)
	}
	for _, name := range []string{"hostapd.conf", "dnsmasq.conf"} {
		if _, err := os.Stat(filepath.Join(g.runtimeDir, name)); err != nil {
			t.Fatalf("AP config %s should remain: %v", name, err)
		}
	}

	manager.mu.Lock()
	manager.connections[speakerPath] = "Speaker-new"
	manager.mu.Unlock()
	if err := g.CleanupAPConfigs(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hostapd.conf", "dnsmasq.conf"} {
		if _, err := os.Stat(filepath.Join(g.runtimeDir, name)); !os.IsNotExist(err) {
			t.Fatalf("AP config %s was not removed: %v", name, err)
		}
	}
	manager.mu.Lock()
	_, speakerExists = manager.connections[speakerPath]
	manager.mu.Unlock()
	if !speakerExists {
		t.Fatal("AP cleanup removed the active Wi-Fi connection")
	}
	if err := g.CleanupWifiConnections(); err != nil {
		t.Fatalf("NetworkManager client was not reusable: %v", err)
	}
	manager.mu.Lock()
	manager.connections[speakerPath] = "Speaker-active"
	manager.mu.Unlock()
	g.Close()
	if first.client.conn.Connected() {
		t.Fatal("NetworkManager connection remains open after Close")
	}
	manager.mu.Lock()
	_, speakerExists = manager.connections[speakerPath]
	manager.mu.Unlock()
	if !speakerExists {
		t.Fatal("Close removed the active Wi-Fi connection")
	}
}
