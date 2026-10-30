//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type displayDevice struct {
	Name        string
	PNPDeviceID string
}

// Match the actual port as well as the display VID/PID. Never restart a hub
// or another USB device while trying to recover a stalled display endpoint.
func recoveryDeviceID(devices []displayDevice, portName string) (string, error) {
	var id string
	for _, d := range devices {
		if !strings.HasSuffix(strings.ToUpper(d.Name), "("+strings.ToUpper(portName)+")") ||
			!strings.HasPrefix(strings.ToUpper(d.PNPDeviceID), `USB\VID_1A86&PID_5722\`) {
			continue
		}
		if id != "" {
			return "", fmt.Errorf("more than one display matches %s", portName)
		}
		id = d.PNPDeviceID
	}
	if id == "" {
		return "", fmt.Errorf("display USB instance for %s not found", portName)
	}
	return id, nil
}

func restartDisplayDevice(portName string) error {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("check USB recovery privileges: %w", err)
	}
	defer token.Close()
	var elevated, returned uint32
	if err := syscall.GetTokenInformation(token, 20 /* TokenElevation */, (*byte)(unsafe.Pointer(&elevated)), 4, &returned); err != nil {
		return fmt.Errorf("check USB recovery privileges: %w", err)
	}
	if elevated == 0 {
		return fmt.Errorf("USB restart requires administrator rights")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	script := `@(Get-CimInstance Win32_PnPEntity -ErrorAction Stop | Where-Object {$_.PNPDeviceID -like 'USB\VID_1A86&PID_5722\*'} | Select-Object Name,PNPDeviceID) | ConvertTo-Json -Compress`
	c := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, err := c.Output()
	if err != nil {
		return fmt.Errorf("identify display: %w", err)
	}
	// ConvertTo-Json unwraps a single-element array on Windows PowerShell.
	var devices []displayDevice
	if err := json.Unmarshal(b, &devices); err != nil {
		var device displayDevice
		if err := json.Unmarshal(b, &device); err != nil {
			return fmt.Errorf("decode display USB instance: %w", err)
		}
		devices = []displayDevice{device}
	}
	id, err := recoveryDeviceID(devices, portName)
	if err != nil {
		return err
	}
	tool := filepath.Join(os.Getenv("SystemRoot"), "System32", "pnputil.exe")
	c = exec.CommandContext(ctx, tool, "/restart-device", id)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, err = c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart %s (requires administrator rights): %w: %s", portName, err, strings.TrimSpace(string(b)))
	}
	return nil
}
