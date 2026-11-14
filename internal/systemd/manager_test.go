package systemd

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

type testSystemd struct {
	conn *dbus.Conn
}

func (s *testSystemd) Subscribe() *dbus.Error { return nil }

func (s *testSystemd) StartUnit(unit, mode string) (dbus.ObjectPath, *dbus.Error) {
	job := dbus.ObjectPath("/org/freedesktop/systemd1/job/1")
	result := "done"
	if unit == "failure.target" {
		result = "failed"
	}
	if err := s.conn.Emit("/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager.JobRemoved", uint32(1), job, unit, result); err != nil {
		return "/", dbus.NewError("org.freedesktop.DBus.Error.Failed", []interface{}{err.Error()})
	}
	return job, nil
}

func (s *testSystemd) StopUnit(unit, mode string) (dbus.ObjectPath, *dbus.Error) {
	return "/", dbus.NewError("org.freedesktop.systemd1.NoSuchUnit", []interface{}{"unit not found"})
}

func TestUnitJobs(t *testing.T) {
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
	defer conn.Close()
	reply, err := conn.RequestName("org.freedesktop.systemd1", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request systemd name: reply=%v err=%v", reply, err)
	}
	service := &testSystemd{conn: conn}
	if err := conn.Export(service, "/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager"); err != nil {
		t.Fatal(err)
	}
	manager, err := NewSystemdManager()
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err := manager.StartUnit("test.target"); err != nil {
		t.Fatalf("start successful job: %v", err)
	}
	if err := manager.StartUnit("failure.target"); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("expected job failure, got %v", err)
	}
	if err := manager.StopUnitIfExists("missing.service"); err != nil {
		t.Fatalf("stop missing unit: %v", err)
	}
}
