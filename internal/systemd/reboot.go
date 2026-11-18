package systemd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

// Reboot asks logind to reboot the system via the system D-Bus.
func Reboot() error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect to D-Bus for reboot: %w", err)
	}
	defer conn.Close()
	log.Print("Requesting reboot via logind D-Bus")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	call := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		CallWithContext(ctx, "org.freedesktop.login1.Manager.Reboot", 0, false)
	if call.Err != nil {
		return fmt.Errorf("request reboot via logind: %w", call.Err)
	}
	return nil
}
