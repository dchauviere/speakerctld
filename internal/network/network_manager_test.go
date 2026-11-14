package network

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

type testNetworkManager struct {
	mu          sync.Mutex
	connections map[dbus.ObjectPath]string
	failDelete  bool
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
		{manager, "/org/freedesktop/NetworkManager/Settings", NMSettingsIface},
		{&testConnection{manager, speakerPath}, speakerPath, NMSettingsIface + ".Connection"},
		{&testConnection{manager, otherPath}, otherPath, NMSettingsIface + ".Connection"},
	} {
		if err := conn.Export(export.value, export.path, export.iface); err != nil {
			t.Fatal(err)
		}
	}
	return manager, speakerPath, otherPath
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
