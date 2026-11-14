package factoryreset

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestResetPersistsUntilNewBoot(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "device"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"device/serial", "wifi", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Request(); err != nil {
		t.Fatal(err)
	}
	if err := m.Request(); err == nil {
		t.Fatal("duplicate reset accepted")
	}
	select {
	case <-m.Requests():
	default:
		t.Fatal("reset not signalled")
	}
	rebootErr := errors.New("reboot failed")
	m.reboot = func() error { return rebootErr }
	if err := m.Execute(); !errors.Is(err, rebootErr) {
		t.Fatalf("expected reboot failure, got %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != markerName {
		t.Fatalf("unexpected contents after reset: %v, %v", entries, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "late-write"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Recover(); !errors.Is(err, rebootErr) {
		t.Fatalf("same-boot recovery must retry reboot: %v", err)
	}
	m.bootID = "next-boot"
	if err := m.Recover(); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("reset not completed: %v, %v", entries, err)
	}
}

func TestMonitorInputRequiresContinuousHold(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	fd := int(reader.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	triggered := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- monitorInput(ctx, fd, 150*time.Millisecond, func() error {
			triggered <- struct{}{}
			return nil
		})
	}()
	send := func(code uint16, value int32) {
		t.Helper()
		if err := binary.Write(writer, binary.NativeEndian, inputEvent{Type: unix.EV_KEY, Code: code, Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	send(keyPower, 1)
	time.Sleep(25 * time.Millisecond)
	send(keyPower, 0)
	time.Sleep(200 * time.Millisecond)
	select {
	case <-triggered:
		t.Fatal("short press triggered reset")
	default:
	}
	send(keyPower, 1)
	select {
	case <-triggered:
	case <-time.After(2 * time.Second):
		t.Fatal("long press did not trigger reset")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestResetRejectsUnsafeDirectories(t *testing.T) {
	if _, err := New("/"); err == nil {
		t.Fatal("filesystem root accepted")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	m, err := New(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Request(); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestHoldDetector(t *testing.T) {
	var d holdDetector
	now := time.Now()
	d.event(unix.EV_KEY, keyPower, 1, now)
	d.event(unix.EV_KEY, keyPower, 2, now.Add(time.Second))
	if d.started != now {
		t.Fatal("repeat reset the hold timer")
	}
	if d.ready(now.Add(999*time.Millisecond), time.Second) || !d.ready(now.Add(time.Second), time.Second) {
		t.Fatal("hold threshold is incorrect")
	}
	d.event(unix.EV_KEY, keyPower, 0, now.Add(2*time.Second))
	if !d.started.IsZero() || d.ready(now.Add(3*time.Second), time.Second) {
		t.Fatal("release did not cancel the hold")
	}
	d.event(unix.EV_KEY, keyPower, 1, now)
	d.event(unix.EV_SYN, synDropped, 0, now)
	d.event(unix.EV_KEY, keyPower, 1, now)
	d.event(unix.EV_SYN, synReport, 0, now)
	if !d.started.IsZero() {
		t.Fatal("lost events did not cancel the hold")
	}
}
