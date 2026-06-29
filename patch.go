package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func printPatchManagement() {
	section("Patch Management")

	printKV(2, "Pending updates", pendingUpdates())
	printKV(2, "Reboot required", rebootRequired())
	printKV(2, "Update source", updateSource())
	printKV(2, "Last update", lastUpdateTime())
}

func pendingUpdates() string {
	switch runtime.GOOS {
	case "linux":
		return linuxPendingUpdates()
	case "windows":
		out, err := powershell(`
$session = New-Object -ComObject Microsoft.Update.Session
$searcher = $session.CreateUpdateSearcher()
$result = $searcher.Search("IsInstalled=0 and Type='Software'")
$result.Updates.Count
`)
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	case "darwin":
		out, err := commandOutput(20*time.Second, "softwareupdate", "-l")
		if err != nil {
			return "N/A"
		}
		count := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "*") {
				count++
			}
		}
		return strconv.Itoa(count)
	default:
		return "N/A"
	}
}

func linuxPendingUpdates() string {
	switch {
	case commandExists("dnf"):
		// Use cached metadata so inventory collection does not unexpectedly block on network refresh.
		out, _ := commandOutput(8*time.Second, "sh", "-c", "dnf -q --cacheonly check-update || true")
		return strconv.Itoa(countDNFUpdates(out))
	case commandExists("apt"):
		out, err := commandOutput(20*time.Second, "apt", "list", "--upgradable")
		if err != nil {
			return "N/A"
		}
		count := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "/") && !strings.HasPrefix(line, "Listing") {
				count++
			}
		}
		return strconv.Itoa(count)
	case commandExists("zypper"):
		out, err := commandOutput(20*time.Second, "zypper", "--non-interactive", "list-updates")
		if err != nil {
			return "N/A"
		}
		count := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "|") && !strings.Contains(line, "Repository") && !strings.Contains(line, "---") {
				count++
			}
		}
		return strconv.Itoa(count)
	case commandExists("pacman"):
		out, err := commandOutput(10*time.Second, "pacman", "-Qu")
		if err != nil && strings.TrimSpace(out) == "" {
			return "0"
		}
		return strconv.Itoa(countNonEmptyLines(out))
	default:
		return "N/A"
	}
}

func countDNFUpdates(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Last metadata") || strings.HasPrefix(line, "Available") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			count++
		}
	}
	return count
}

func rebootRequired() string {
	switch runtime.GOOS {
	case "linux":
		if fileExists("/var/run/reboot-required") || fileExists("/run/reboot-required") {
			return "Yes"
		}
		if commandExists("needs-restarting") {
			_, err := commandOutput(8*time.Second, "needs-restarting", "-r")
			if err != nil {
				return "Yes"
			}
			return "No"
		}
		return "N/A"
	case "windows":
		out, err := powershell(`
$paths = @(
  "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending",
  "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired"
)
if ($paths | Where-Object { Test-Path $_ }) { "Yes" } else { "No" }
`)
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	default:
		return "N/A"
	}
}

func updateSource() string {
	switch runtime.GOOS {
	case "linux":
		for _, manager := range []string{"dnf", "apt", "zypper", "pacman", "rpm-ostree"} {
			if commandExists(manager) {
				return manager
			}
		}
	case "windows":
		return "Windows Update"
	case "darwin":
		return "softwareupdate"
	}
	return "N/A"
}

func lastUpdateTime() string {
	switch runtime.GOOS {
	case "linux":
		for _, path := range []string{"/var/log/dnf.rpm.log", "/var/log/apt/history.log", "/var/log/pacman.log", "/var/log/zypp/history"} {
			if value := lastModifiedTime(path); value != "" {
				return value
			}
		}
	case "windows":
		out, err := powershell(`$h = Get-HotFix | Sort-Object InstalledOn -Descending | Select-Object -First 1; if ($h) { "$($h.HotFixID) $($h.InstalledOn)" } else { "N/A" }`)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	case "darwin":
		if value := lastModifiedTime("/Library/Receipts/InstallHistory.plist"); value != "" {
			return value
		}
	}
	return "N/A"
}

func lastModifiedTime(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return info.ModTime().Format(time.RFC1123)
}
