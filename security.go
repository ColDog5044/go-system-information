package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func printSecurityPosture() {
	section("Security Posture")

	printKV(2, "Firewall", firewallPosture())
	printKV(2, "AV/EDR", avEDRPosture())
	printKV(2, "Secure Boot", secureBootPosture())
	printKV(2, "Disk encryption", diskEncryptionPosture())
	printKV(2, "Failed logins", failedLoginSummary())
}

func firewallPosture() string {
	switch runtime.GOOS {
	case "linux":
		return linuxFirewallPosture()
	case "windows":
		return windowsFirewallPosture()
	case "darwin":
		return darwinFirewallPosture()
	default:
		return "N/A"
	}
}

func linuxFirewallPosture() string {
	if commandExists("firewall-cmd") {
		state, err := commandOutput(3*time.Second, "firewall-cmd", "--state")
		if err == nil {
			zone, _ := commandOutput(3*time.Second, "firewall-cmd", "--get-default-zone")
			return fmt.Sprintf("firewalld %s, default zone %s", strings.TrimSpace(state), firstNonEmpty(zone, "N/A"))
		}
	}
	if commandExists("ufw") {
		out, err := commandOutput(3*time.Second, "ufw", "status")
		if err == nil {
			lines := nonEmptyLines(out)
			if len(lines) > 0 {
				return lines[0]
			}
		}
	}
	if commandExists("nft") {
		out, err := commandOutput(3*time.Second, "nft", "list", "ruleset")
		if err == nil && strings.TrimSpace(out) != "" {
			return "nftables rules loaded"
		}
	}
	return "Not detected"
}

func windowsFirewallPosture() string {
	out, err := powershell(`
Get-NetFirewallProfile | ForEach-Object {
  "$($_.Name): Enabled=$($_.Enabled), DefaultInbound=$($_.DefaultInboundAction)"
}
`)
	if err != nil {
		return "N/A"
	}
	return strings.Join(nonEmptyLines(out), "; ")
}

func darwinFirewallPosture() string {
	out, err := commandOutput(3*time.Second, "/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate")
	if err == nil {
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	}
	return "N/A"
}

func avEDRPosture() string {
	switch runtime.GOOS {
	case "linux":
		return linuxAVEDRPosture()
	case "windows":
		return windowsAVEDRPosture()
	case "darwin":
		return darwinAVEDRPosture()
	default:
		return "N/A"
	}
}

func linuxAVEDRPosture() string {
	var detected []string
	serviceNames := []string{
		"clamav-daemon", "clamd", "falcon-sensor", "sentinelone", "wazuh-agent",
		"ossec", "mdatp", "defender", "qualys-cloud-agent", "nessusagent",
	}
	for _, svc := range serviceNames {
		if systemctlUnitExists(svc + ".service") {
			state := systemctlUnitState(svc + ".service")
			detected = append(detected, fmt.Sprintf("%s (%s)", svc, firstNonEmpty(state, "unknown")))
		}
	}
	if commandExists("mdatp") {
		out, err := commandOutput(5*time.Second, "mdatp", "health")
		if err == nil {
			detected = append(detected, "Microsoft Defender: "+summarizeDelimitedLines(out, 3))
		} else {
			detected = append(detected, "Microsoft Defender detected")
		}
	}
	if len(detected) == 0 {
		return "Not detected"
	}
	return strings.Join(uniqueStrings(detected), "; ")
}

func windowsAVEDRPosture() string {
	out, err := powershell(`
$products = Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntivirusProduct -ErrorAction SilentlyContinue
if ($products) {
  $products | ForEach-Object { "$($_.displayName) state=$($_.productState)" }
} else {
  "Not detected"
}
`)
	if err != nil {
		return "N/A"
	}
	return strings.Join(nonEmptyLines(out), "; ")
}

func darwinAVEDRPosture() string {
	var detected []string
	if commandExists("mdatp") {
		out, err := commandOutput(5*time.Second, "mdatp", "health")
		if err == nil {
			detected = append(detected, "Microsoft Defender: "+summarizeDelimitedLines(out, 3))
		} else {
			detected = append(detected, "Microsoft Defender detected")
		}
	}
	if commandExists("systemextensionsctl") {
		out, err := commandOutput(5*time.Second, "systemextensionsctl", "list")
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				lower := strings.ToLower(line)
				if strings.Contains(lower, "endpoint") || strings.Contains(lower, "network extension") {
					detected = append(detected, strings.TrimSpace(line))
				}
			}
		}
	}
	if len(detected) == 0 {
		return "Not detected"
	}
	return strings.Join(uniqueStrings(detected), "; ")
}

func secureBootPosture() string {
	switch runtime.GOOS {
	case "linux":
		return linuxSecureBootPosture()
	case "windows":
		out, err := powershell(`try { if (Confirm-SecureBootUEFI) { "Enabled" } else { "Disabled" } } catch { "Unsupported" }`)
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	case "darwin":
		out, err := commandOutput(5*time.Second, "system_profiler", "SPiBridgeDataType")
		if err == nil && strings.TrimSpace(out) != "" {
			values := parseKeyValueLines(out)
			return firstNonEmpty(values["Secure Boot"], values["Boot Policy"], "N/A")
		}
		return "N/A"
	default:
		return "N/A"
	}
}

func linuxSecureBootPosture() string {
	if commandExists("mokutil") {
		out, err := commandOutput(3*time.Second, "mokutil", "--sb-state")
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	}
	matches, _ := filepath.Glob("/sys/firmware/efi/efivars/SecureBoot-*")
	if len(matches) == 0 {
		return "Unsupported"
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || len(data) < 5 {
		return "N/A"
	}
	if data[4] == 1 {
		return "Enabled"
	}
	return "Disabled"
}

func diskEncryptionPosture() string {
	switch runtime.GOOS {
	case "linux":
		return linuxDiskEncryptionPosture()
	case "windows":
		out, err := powershell(`Get-BitLockerVolume -ErrorAction SilentlyContinue | ForEach-Object { "$($_.MountPoint) $($_.VolumeStatus) Protection=$($_.ProtectionStatus)" }`)
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.Join(nonEmptyLines(out), "; "), "Not detected")
	case "darwin":
		out, err := commandOutput(5*time.Second, "fdesetup", "status")
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	default:
		return "N/A"
	}
}

func linuxDiskEncryptionPosture() string {
	out, err := commandOutput(5*time.Second, "lsblk", "-P", "-o", "NAME,TYPE,FSTYPE,MOUNTPOINT")
	if err != nil {
		return "N/A"
	}
	var encrypted []string
	for _, line := range strings.Split(out, "\n") {
		values := parseShellKeyValueLine(line)
		fstype := strings.ToLower(values["FSTYPE"])
		kind := strings.ToLower(values["TYPE"])
		if strings.Contains(fstype, "crypto") || kind == "crypt" {
			encrypted = append(encrypted, firstNonEmpty(values["MOUNTPOINT"], values["NAME"], "encrypted volume"))
		}
	}
	if len(encrypted) == 0 {
		return "Not detected"
	}
	return "Detected: " + strings.Join(uniqueStrings(encrypted), ", ")
}

func failedLoginSummary() string {
	switch runtime.GOOS {
	case "linux":
		return linuxFailedLoginSummary()
	case "windows":
		out, err := powershell(`
$start = (Get-Date).AddDays(-1)
$count = (Get-WinEvent -FilterHashtable @{LogName='Security'; Id=4625; StartTime=$start} -ErrorAction SilentlyContinue | Measure-Object).Count
"Last 24h: $count"
`)
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	case "darwin":
		out, err := commandOutput(5*time.Second, "log", "show", "--last", "1d", "--predicate", "eventMessage CONTAINS[c] \"authentication failed\"", "--style", "compact")
		if err != nil {
			return "N/A"
		}
		return fmt.Sprintf("Last 24h: %d", countNonEmptyLines(out))
	default:
		return "N/A"
	}
}

func linuxFailedLoginSummary() string {
	if commandExists("lastb") {
		out, err := commandOutput(5*time.Second, "lastb", "-n", "100", "-w")
		if err == nil {
			count := 0
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.Contains(strings.ToLower(line), "btmp begins") {
					count++
				}
			}
			return fmt.Sprintf("Recent btmp entries: %d", count)
		}
	}
	if commandExists("faillock") {
		out, err := commandOutput(5*time.Second, "faillock")
		if err == nil {
			return fmt.Sprintf("faillock entries: %d", countNonEmptyLines(out))
		}
	}
	return "N/A"
}

func systemctlUnitExists(name string) bool {
	if !commandExists("systemctl") {
		return false
	}
	_, err := commandOutput(3*time.Second, "systemctl", "status", name)
	return err == nil
}

func systemctlUnitState(name string) string {
	if !commandExists("systemctl") {
		return ""
	}
	out, err := commandOutput(3*time.Second, "systemctl", "is-active", name)
	if err == nil {
		return strings.TrimSpace(out)
	}
	out, err = commandOutput(3*time.Second, "systemctl", "is-enabled", name)
	if err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func summarizeDelimitedLines(output string, limit int) string {
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		return "detected"
	}
	if len(lines) > limit {
		lines = lines[:limit]
	}
	for i, line := range lines {
		lines[i] = truncateString(strings.TrimSpace(line), 80)
	}
	return strings.Join(lines, "; ")
}

func parseShellKeyValueLine(line string) map[string]string {
	values := map[string]string{}
	fields := strings.Fields(line)
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			values[key] = strings.Trim(value, `"`)
		}
	}
	return values
}
