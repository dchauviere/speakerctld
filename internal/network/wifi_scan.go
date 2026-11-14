package network

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/godbus/dbus/v5"
)

const (
	wirelessDeviceInterface = NMDeviceIface + ".Wireless"
	accessPointInterface    = NMInterface + ".AccessPoint"
	apPrivacy               = uint32(0x1)
	apKeyMgmtPSK            = uint32(0x100)
)

type WifiNetwork struct {
	SSID          string `json:"ssid"`
	SignalPercent uint8  `json:"signalPercent"`
	Security      string `json:"security"`
}

// ScanWifiNetworks requests a scan on the selected interface and waits for it to finish.
func (nmc *NetworkManagerClient) ScanWifiNetworks(ctx context.Context, iface string) ([]WifiNetwork, error) {
	device, err := nmc.findWifiDevice(ctx, iface)
	if err != nil {
		return nil, err
	}
	obj := nmc.conn.Object(NMService, device)
	readLastScan := func() (int64, error) {
		var value dbus.Variant
		call := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, wirelessDeviceInterface, "LastScan")
		if call.Err != nil {
			return 0, call.Err
		}
		if err := call.Store(&value); err != nil {
			return 0, err
		}
		lastScan, ok := value.Value().(int64)
		if !ok {
			return 0, fmt.Errorf("invalid LastScan property")
		}
		return lastScan, nil
	}
	lastScan, err := readLastScan()
	if err != nil {
		return nil, fmt.Errorf("read last Wi-Fi scan: %w", err)
	}
	call := obj.CallWithContext(ctx, wirelessDeviceInterface+".RequestScan", 0, map[string]dbus.Variant{})
	if call.Err != nil {
		return nil, fmt.Errorf("request Wi-Fi scan: %w", call.Err)
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := readLastScan()
		if err != nil {
			return nil, fmt.Errorf("wait for Wi-Fi scan: %w", err)
		}
		if current > lastScan {
			break
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for Wi-Fi scan: %w", ctx.Err())
		case <-ticker.C:
		}
	}

	var paths []dbus.ObjectPath
	call = obj.CallWithContext(ctx, wirelessDeviceInterface+".GetAllAccessPoints", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("list Wi-Fi access points: %w", call.Err)
	}
	if err := call.Store(&paths); err != nil {
		return nil, fmt.Errorf("read Wi-Fi access points: %w", err)
	}

	networks := make(map[string]WifiNetwork)
	for _, path := range paths {
		var props map[string]dbus.Variant
		call = nmc.conn.Object(NMService, path).CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, accessPointInterface)
		if call.Err != nil {
			if dbusErr, ok := call.Err.(dbus.Error); ok && dbusErr.Name == "org.freedesktop.DBus.Error.UnknownObject" {
				log.Printf("Wi-Fi access point %s disappeared during scan", path)
				continue
			}
			return nil, fmt.Errorf("read Wi-Fi access point %s: %w", path, call.Err)
		}
		if err := call.Store(&props); err != nil {
			return nil, fmt.Errorf("parse Wi-Fi access point %s: %w", path, err)
		}
		ssid, ssidOK := props["Ssid"].Value().([]byte)
		strength, strengthOK := props["Strength"].Value().(byte)
		flags, flagsOK := props["Flags"].Value().(uint32)
		wpa, wpaOK := props["WpaFlags"].Value().(uint32)
		rsn, rsnOK := props["RsnFlags"].Value().(uint32)
		mode, modeOK := props["Mode"].Value().(uint32)
		if !ssidOK || !strengthOK || !flagsOK || !wpaOK || !rsnOK || !modeOK {
			return nil, fmt.Errorf("invalid properties for Wi-Fi access point %s", path)
		}
		if mode != 2 || len(ssid) == 0 || !utf8.Valid(ssid) || strings.ContainsRune(string(ssid), 0) {
			continue
		}
		security := "unsupported"
		switch {
		case (wpa|rsn)&apKeyMgmtPSK != 0:
			security = "wpa-psk"
		case flags&apPrivacy == 0 && wpa == 0 && rsn == 0:
			security = "open"
		}
		network := WifiNetwork{SSID: string(ssid), SignalPercent: strength, Security: security}
		key := network.SSID + "\x00" + security
		if old, ok := networks[key]; !ok || strength > old.SignalPercent {
			networks[key] = network
		}
	}
	result := make([]WifiNetwork, 0, len(networks))
	for _, network := range networks {
		result = append(result, network)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SignalPercent != result[j].SignalPercent {
			return result[i].SignalPercent > result[j].SignalPercent
		}
		if result[i].SSID != result[j].SSID {
			return result[i].SSID < result[j].SSID
		}
		return result[i].Security < result[j].Security
	})
	return result, nil
}

func (wcm *WifiConnectionManager) ScanWifiNetworks(ctx context.Context, iface string) ([]WifiNetwork, error) {
	return wcm.client.ScanWifiNetworks(ctx, iface)
}
