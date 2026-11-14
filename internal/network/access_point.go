package network

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

const activeConnectionInterface = "org.freedesktop.NetworkManager.Connection.Active"

// StartAccessPoint activates an ephemeral NetworkManager AP with a static IPv4 address.
func (nmc *NetworkManagerClient) StartAccessPoint(ctx context.Context, iface, ssid, password, address string) error {
	if iface == "" || len(ssid) == 0 || len(ssid) > 32 {
		return fmt.Errorf("invalid access point interface or SSID")
	}
	if password != "" && (len(password) < 8 || len(password) > 63) {
		return fmt.Errorf("invalid access point password length")
	}

	device, err := nmc.findWifiDevice(ctx, iface)
	if err != nil {
		return err
	}

	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":             dbus.MakeVariant(ConnectionPrefix + "AP"),
			"type":           dbus.MakeVariant(ConnectionTypeWireless),
			"interface-name": dbus.MakeVariant(iface),
			"autoconnect":    dbus.MakeVariant(false),
		},
		"802-11-wireless": {
			"ssid": dbus.MakeVariant([]byte(ssid)),
			"mode": dbus.MakeVariant("ap"),
			"band": dbus.MakeVariant("bg"),
		},
		"ipv4": {
			"method": dbus.MakeVariant("manual"),
			"address-data": dbus.MakeVariant([]map[string]dbus.Variant{{
				"address": dbus.MakeVariant(address),
				"prefix":  dbus.MakeVariant(uint32(24)),
			}}),
			"never-default": dbus.MakeVariant(true),
		},
		"ipv6": {"method": dbus.MakeVariant("disabled")},
	}
	if password != "" {
		settings["802-11-wireless-security"] = map[string]dbus.Variant{
			"key-mgmt": dbus.MakeVariant(SecurityTypeWPA),
			"psk":      dbus.MakeVariant(password),
			"proto":    dbus.MakeVariant([]string{"rsn"}),
			"pairwise": dbus.MakeVariant([]string{"ccmp"}),
		}
	}
	options := map[string]dbus.Variant{"persist": dbus.MakeVariant("volatile")}
	call := nmc.nmObj.CallWithContext(ctx, NMInterface+".AddAndActivateConnection2", 0,
		settings, device, dbus.ObjectPath("/"), options)
	if call.Err != nil {
		return fmt.Errorf("activate access point on %s: %w", iface, call.Err)
	}
	var connection, active dbus.ObjectPath
	var result map[string]dbus.Variant
	if err := call.Store(&connection, &active, &result); err != nil {
		return fmt.Errorf("parse access point activation: %w", err)
	}

	if err := nmc.waitForAccessPoint(ctx, active); err != nil {
		rollback := nmc.nmObj.Call(NMInterface+".DeactivateConnection", 0, active)
		if rollback.Err != nil {
			return errors.Join(err, fmt.Errorf("deactivate failed access point: %w", rollback.Err))
		}
		return err
	}
	log.Printf("NetworkManager access point activated on %s with address %s/24", iface, address)
	return nil
}

func (nmc *NetworkManagerClient) findWifiDevice(ctx context.Context, iface string) (dbus.ObjectPath, error) {
	var device dbus.ObjectPath
	call := nmc.nmObj.CallWithContext(ctx, NMInterface+".GetDeviceByIpIface", 0, iface)
	if call.Err != nil {
		return "", fmt.Errorf("find interface %s: %w", iface, call.Err)
	}
	if err := call.Store(&device); err != nil {
		return "", fmt.Errorf("parse device for %s: %w", iface, err)
	}
	var deviceType dbus.Variant
	call = nmc.conn.Object(NMService, device).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, NMDeviceIface, "DeviceType")
	if call.Err != nil {
		return "", fmt.Errorf("get device type for %s: %w", iface, call.Err)
	}
	if err := call.Store(&deviceType); err != nil {
		return "", fmt.Errorf("parse device type for %s: %w", iface, err)
	}
	if deviceType.Value() != NMDeviceTypeWifi {
		return "", fmt.Errorf("interface %s is not a Wi-Fi device", iface)
	}
	return device, nil
}

func (nmc *NetworkManagerClient) waitForAccessPoint(ctx context.Context, active dbus.ObjectPath) error {
	obj := nmc.conn.Object(NMService, active)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		call := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, activeConnectionInterface, "State")
		if call.Err != nil {
			return fmt.Errorf("read access point activation state: %w", call.Err)
		}
		var value dbus.Variant
		if err := call.Store(&value); err != nil {
			return fmt.Errorf("parse access point activation state: %w", err)
		}
		state, ok := value.Value().(uint32)
		if !ok {
			return fmt.Errorf("invalid access point activation state: %v", value.Value())
		}
		switch state {
		case 2:
			return nil
		case 3, 4:
			return fmt.Errorf("access point activation failed (state %d)", state)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for access point activation: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
