package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func printOperationalInventory() {
	section("Operational Inventory")

	printKV(2, "Scheduled work", scheduledWorkInventory())
	printKV(2, "Certificates", certificateInventory())
	printKV(2, "Printers", printerInventory())
	printKV(2, "Shares/mapped drives", shareInventory())
	printKV(2, "Remote access", remoteAccessInventory())
}

func scheduledWorkInventory() string {
	switch runtime.GOOS {
	case "linux":
		var parts []string
		if commandExists("systemctl") {
			out, err := commandOutput(8*time.Second, "systemctl", "list-timers", "--all", "--no-legend", "--plain")
			if err == nil {
				parts = append(parts, fmt.Sprintf("systemd timers=%d", countNonEmptyLines(out)))
			}
		}
		cronCount := countCronFiles()
		parts = append(parts, fmt.Sprintf("cron files=%d", cronCount))
		return strings.Join(parts, ", ")
	case "windows":
		out, err := powershell(`
$tasks = Get-ScheduledTask
$disabled = ($tasks | Where-Object { $_.State -eq 'Disabled' } | Measure-Object).Count
"tasks=$($tasks.Count), disabled=$disabled"
`)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	case "darwin":
		out, err := commandOutput(8*time.Second, "launchctl", "list")
		if err == nil {
			return fmt.Sprintf("launchd jobs=%d", countNonEmptyLines(out)-1)
		}
	}
	return "N/A"
}

func countCronFiles() int {
	paths := []string{"/etc/crontab", "/etc/cron.d", "/etc/cron.daily", "/etc/cron.hourly", "/etc/cron.monthly", "/etc/cron.weekly", "/var/spool/cron", "/var/spool/cron/crontabs"}
	count := 0
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			count++
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				count++
			}
		}
	}
	return count
}

func certificateInventory() string {
	switch runtime.GOOS {
	case "linux":
		return linuxCertificateInventory()
	case "windows":
		out, err := powershell(`
$now = Get-Date
$soon = $now.AddDays(30)
$certs = Get-ChildItem Cert:\LocalMachine\My,Cert:\LocalMachine\Root -ErrorAction SilentlyContinue
$expired = ($certs | Where-Object { $_.NotAfter -lt $now } | Measure-Object).Count
$expiring = ($certs | Where-Object { $_.NotAfter -ge $now -and $_.NotAfter -le $soon } | Measure-Object).Count
"total=$($certs.Count), expired=$expired, expiring_30d=$expiring"
`)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	case "darwin":
		out, err := commandOutput(10*time.Second, "security", "find-certificate", "-a", "-p", "/Library/Keychains/System.keychain")
		if err == nil {
			return summarizePEMCertificates([]byte(out))
		}
	}
	return "N/A"
}

func linuxCertificateInventory() string {
	paths := []string{"/etc/ssl/certs", "/etc/pki/tls/certs"}
	total := 0
	expired := 0
	expiring := 0
	checkedFiles := 0
	now := time.Now()
	soon := now.Add(30 * 24 * time.Hour)

	for _, root := range paths {
		if !fileExists(root) {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || checkedFiles >= 500 {
				return nil
			}
			checkedFiles++
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			certs := parsePEMCertificates(data)
			for _, cert := range certs {
				total++
				if cert.NotAfter.Before(now) {
					expired++
				} else if cert.NotAfter.Before(soon) {
					expiring++
				}
			}
			return nil
		})
	}
	if total == 0 {
		return "N/A"
	}
	return fmt.Sprintf("total=%d, expired=%d, expiring_30d=%d", total, expired, expiring)
}

func summarizePEMCertificates(data []byte) string {
	total := 0
	expired := 0
	expiring := 0
	now := time.Now()
	soon := now.Add(30 * 24 * time.Hour)
	for _, cert := range parsePEMCertificates(data) {
		total++
		if cert.NotAfter.Before(now) {
			expired++
		} else if cert.NotAfter.Before(soon) {
			expiring++
		}
	}
	if total == 0 {
		return "N/A"
	}
	return fmt.Sprintf("total=%d, expired=%d, expiring_30d=%d", total, expired, expiring)
}

func parsePEMCertificates(data []byte) []*x509.Certificate {
	var certs []*x509.Certificate
	for {
		block, rest := pem.Decode(data)
		if block == nil {
			break
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err == nil {
			certs = append(certs, cert)
		}
	}
	return certs
}

func printerInventory() string {
	switch runtime.GOOS {
	case "linux", "darwin":
		if commandExists("lpstat") {
			out, err := commandOutput(5*time.Second, "lpstat", "-p")
			if err == nil {
				lines := nonEmptyLines(out)
				if len(lines) == 0 {
					return "Not detected"
				}
				return fmt.Sprintf("%d printers: %s", len(lines), strings.Join(limitStrings(lines, 5), "; "))
			}
		}
	case "windows":
		out, err := powershell(`Get-Printer | ForEach-Object { "$($_.Name) ($($_.PrinterStatus))" }`)
		if err == nil {
			lines := nonEmptyLines(out)
			if len(lines) == 0 {
				return "Not detected"
			}
			return fmt.Sprintf("%d printers: %s", len(lines), strings.Join(limitStrings(lines, 5), "; "))
		}
	}
	return "N/A"
}

func shareInventory() string {
	switch runtime.GOOS {
	case "linux", "darwin":
		return unixShareInventory()
	case "windows":
		out, err := powershell(`
$shares = Get-SmbShare -ErrorAction SilentlyContinue | Where-Object { -not $_.Special }
$maps = Get-SmbMapping -ErrorAction SilentlyContinue
"shares=$($shares.Count), mapped=$($maps.Count)"
`)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	}
	return "N/A"
}

func unixShareInventory() string {
	mounts := readFile("/proc/mounts")
	if runtime.GOOS == "darwin" {
		out, err := commandOutput(3*time.Second, "mount")
		if err == nil {
			mounts = out
		}
	}
	if mounts == "" {
		return "N/A"
	}
	count := 0
	var examples []string
	for _, line := range strings.Split(mounts, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		fstype := strings.ToLower(fields[2])
		if fstype == "cifs" || fstype == "smbfs" || fstype == "nfs" || strings.HasPrefix(fstype, "nfs") {
			count++
			examples = append(examples, fields[1]+" ("+fields[2]+")")
		}
	}
	if count == 0 {
		return "Not detected"
	}
	return fmt.Sprintf("%d mounts: %s", count, strings.Join(limitStrings(examples, 5), "; "))
}

func remoteAccessInventory() string {
	switch runtime.GOOS {
	case "linux":
		return linuxRemoteAccessInventory()
	case "windows":
		return windowsRemoteAccessInventory()
	case "darwin":
		return darwinRemoteAccessInventory()
	default:
		return "N/A"
	}
}

func linuxRemoteAccessInventory() string {
	var parts []string
	for _, svc := range []string{"sshd.service", "ssh.service", "xrdp.service", "vncserver.service"} {
		if systemctlUnitExists(svc) {
			parts = append(parts, svc+"="+firstNonEmpty(systemctlUnitState(svc), "detected"))
		}
	}
	for _, port := range listeningPorts() {
		if port.Port == "22" || port.Port == "3389" || port.Port == "5900" || port.Port == "5901" {
			parts = append(parts, listeningPortLine(port))
		}
	}
	if len(parts) == 0 {
		return "Not detected"
	}
	return strings.Join(uniqueStrings(parts), "; ")
}

func windowsRemoteAccessInventory() string {
	out, err := powershell(`
$rdp = (Get-ItemProperty "HKLM:\System\CurrentControlSet\Control\Terminal Server").fDenyTSConnections
$winrm = Get-Service WinRM -ErrorAction SilentlyContinue
$ssh = Get-Service sshd -ErrorAction SilentlyContinue
"RDP=$([bool]($rdp -eq 0)); WinRM=$($winrm.Status); OpenSSH=$($ssh.Status)"
`)
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(out), "N/A")
}

func darwinRemoteAccessInventory() string {
	var parts []string
	out, err := commandOutput(5*time.Second, "systemsetup", "-getremotelogin")
	if err == nil {
		parts = append(parts, strings.TrimSpace(out))
	}
	for _, port := range listeningPorts() {
		if port.Port == "22" || port.Port == "5900" {
			parts = append(parts, listeningPortLine(port))
		}
	}
	if len(parts) == 0 {
		return "N/A"
	}
	return strings.Join(uniqueStrings(parts), "; ")
}

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return append(values[:limit], fmt.Sprintf("... %d more", len(values)-limit))
}
