package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func printHardwareHealth() {
	section("Hardware Health")

	printKV(2, "Storage health", storageHealth())
	printKV(2, "Battery", batteryHealth())
	printKV(2, "GPU", gpuInventory())
	printKV(2, "Monitors", monitorInventory())
	printKV(2, "Motherboard", motherboardInventory())
}

func storageHealth() string {
	switch runtime.GOOS {
	case "linux", "darwin":
		return smartctlStorageHealth()
	case "windows":
		out, err := powershell(`Get-PhysicalDisk | ForEach-Object { "$($_.FriendlyName): $($_.HealthStatus), Operational=$($_.OperationalStatus -join '/')" }`)
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.Join(nonEmptyLines(out), "; "), "N/A")
	default:
		return "N/A"
	}
}

func smartctlStorageHealth() string {
	if !commandExists("smartctl") {
		return "N/A (smartctl not found)"
	}
	out, err := commandOutput(5*time.Second, "smartctl", "--scan")
	if err != nil {
		return "N/A"
	}

	var results []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		device := fields[0]
		health, healthErr := commandOutput(8*time.Second, "smartctl", "-H", device)
		if healthErr != nil {
			results = append(results, fmt.Sprintf("%s: N/A", device))
			continue
		}
		status := "Unknown"
		for _, healthLine := range strings.Split(health, "\n") {
			if strings.Contains(strings.ToLower(healthLine), "overall-health") || strings.Contains(strings.ToLower(healthLine), "smart health") {
				if _, value, ok := strings.Cut(healthLine, ":"); ok {
					status = strings.TrimSpace(value)
				}
			}
		}
		results = append(results, fmt.Sprintf("%s: %s", device, status))
	}
	if len(results) == 0 {
		return "N/A"
	}
	return strings.Join(results, "; ")
}

func batteryHealth() string {
	var parts []string
	switch runtime.GOOS {
	case "linux":
		parts = append(parts, linuxBatteryHealth()...)
	case "windows":
		out, err := powershell(`Get-CimInstance Win32_Battery | ForEach-Object { "$($_.Name): $($_.EstimatedChargeRemaining)% status=$($_.BatteryStatus)" }`)
		if err == nil {
			parts = append(parts, nonEmptyLines(out)...)
		}
	case "darwin":
		out, err := commandOutput(3*time.Second, "pmset", "-g", "batt")
		if err == nil {
			parts = append(parts, nonEmptyLines(out)...)
		}
	}
	if len(parts) == 0 {
		return "Not detected"
	}
	return strings.Join(parts, "; ")
}

func linuxBatteryHealth() []string {
	dirs, _ := filepath.Glob("/sys/class/power_supply/*")
	var results []string
	for _, dir := range dirs {
		kind := strings.ToLower(readFile(filepath.Join(dir, "type")))
		if kind != "battery" {
			continue
		}
		name := filepath.Base(dir)
		capacity := readFile(filepath.Join(dir, "capacity"))
		status := readFile(filepath.Join(dir, "status"))
		health := readFile(filepath.Join(dir, "health"))
		results = append(results, fmt.Sprintf("%s: %s%% %s %s", name, firstNonEmpty(capacity, "N/A"), firstNonEmpty(status, "N/A"), firstNonEmpty(health, "")))
	}
	return results
}

func gpuInventory() string {
	switch runtime.GOOS {
	case "linux":
		if commandExists("lspci") {
			out, err := commandOutput(5*time.Second, "lspci")
			if err == nil {
				var gpus []string
				for _, line := range strings.Split(out, "\n") {
					lower := strings.ToLower(line)
					if strings.Contains(lower, "vga compatible controller") || strings.Contains(lower, "3d controller") || strings.Contains(lower, "display controller") {
						gpus = append(gpus, truncateString(strings.TrimSpace(line), 120))
					}
				}
				if len(gpus) > 0 {
					return strings.Join(gpus, "; ")
				}
			}
		}
	case "windows":
		out, err := powershell(`Get-CimInstance Win32_VideoController | ForEach-Object { "$($_.Name) ($([math]::Round($_.AdapterRAM / 1GB, 2)) GB)" }`)
		if err == nil {
			return firstNonEmpty(strings.Join(nonEmptyLines(out), "; "), "N/A")
		}
	case "darwin":
		out, err := commandOutput(8*time.Second, "system_profiler", "SPDisplaysDataType")
		if err == nil {
			return summarizeSystemProfilerNames(out, 5)
		}
	}
	return "N/A"
}

func monitorInventory() string {
	switch runtime.GOOS {
	case "linux":
		matches, _ := filepath.Glob("/sys/class/drm/card*-*/status")
		var connected []string
		for _, statusPath := range matches {
			if strings.TrimSpace(readFile(statusPath)) == "connected" {
				connected = append(connected, filepath.Base(filepath.Dir(statusPath)))
			}
		}
		if len(connected) == 0 {
			return "Not detected"
		}
		return strings.Join(connected, ", ")
	case "windows":
		out, err := powershell(`Get-CimInstance -Namespace root\wmi -ClassName WmiMonitorID -ErrorAction SilentlyContinue | ForEach-Object { ([System.Text.Encoding]::ASCII.GetString(($_.UserFriendlyName | Where-Object { $_ -ne 0 }))) }`)
		if err == nil {
			return firstNonEmpty(strings.Join(nonEmptyLines(out), "; "), "N/A")
		}
	case "darwin":
		out, err := commandOutput(8*time.Second, "system_profiler", "SPDisplaysDataType")
		if err == nil {
			return summarizeSystemProfilerDisplays(out)
		}
	}
	return "N/A"
}

func motherboardInventory() string {
	switch runtime.GOOS {
	case "linux":
		board := strings.Join(nonEmptyStrings(
			cleanInventoryValue(readDMI("board_vendor")),
			cleanInventoryValue(readDMI("board_name")),
			cleanInventoryValue(readDMI("board_version")),
			cleanInventoryValue(readDMI("board_serial")),
		), " ")
		return firstNonEmpty(board, "N/A")
	case "windows":
		out, err := powershell(`
$board = Get-CimInstance Win32_BaseBoard | Select-Object -First 1
"$($board.Manufacturer) $($board.Product) $($board.Version) $($board.SerialNumber)"
`)
		if err == nil {
			return firstNonEmpty(strings.Join(nonEmptyLines(out), "; "), "N/A")
		}
	case "darwin":
		out, err := commandOutput(8*time.Second, "system_profiler", "SPHardwareDataType")
		if err == nil {
			values := parseKeyValueLines(out)
			return strings.Join(nonEmptyStrings(values["Model Name"], values["Model Identifier"]), "; ")
		}
	}
	return "N/A"
}

func summarizeSystemProfilerNames(output string, limit int) string {
	var names []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, "Displays") && !strings.Contains(trimmed, "Display") {
			names = append(names, strings.TrimSuffix(trimmed, ":"))
		}
	}
	if len(names) == 0 {
		return "N/A"
	}
	if len(names) > limit {
		names = names[:limit]
	}
	return strings.Join(names, "; ")
}

func summarizeSystemProfilerDisplays(output string) string {
	var displays []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, ":") && strings.Contains(strings.ToLower(trimmed), "display") {
			displays = append(displays, strings.TrimSuffix(trimmed, ":"))
		}
	}
	if len(displays) == 0 {
		return summarizeSystemProfilerNames(output, 5)
	}
	return strings.Join(displays, "; ")
}
