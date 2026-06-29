package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func printSystem(samples sampleData) {
	section("System")

	inventory := getSystemInventory()
	printKV(2, "OS", osDescription())
	printKV(2, "Boot time", bootTimeString())
	printKV(2, "Uptime", uptimeString())
	printKV(2, "BIOS", inventory.bios)
	if runtime.GOOS == "windows" {
		printKV(2, "Product key", inventory.productKey)
	}
	printKV(2, "TPM", tpmStatus())
	printKV(2, "Domain", domainName())
	printKV(2, "Timezone", timezoneString())
	printKV(2, "Manufacturer", inventory.manufacturer)
	printKV(2, "Model", inventory.model)
	printKV(2, "Serial number", inventory.serialNumber)
	printKV(2, "Device name/hostname", hostname())
	printKV(2, "Inactive time", currentUserIdleTime())
	printKV(2, "CPU idle since boot", cpuIdleSinceBoot())
	printKV(2, "Last login", lastLogin())
	printKV(2, "Rate sample interval", fmt.Sprintf("%.0fs", samples.intervalSeconds))
}

func getSystemInventory() systemInventory {
	inventory := systemInventory{
		bios:         "N/A",
		productKey:   "N/A",
		manufacturer: "N/A",
		model:        "N/A",
		serialNumber: "N/A",
	}

	switch runtime.GOOS {
	case "windows":
		values := windowsInventoryValues()
		inventory.manufacturer = cleanInventoryValue(values["Manufacturer"])
		inventory.model = cleanInventoryValue(values["Model"])
		inventory.serialNumber = cleanInventoryValue(values["SerialNumber"])
		inventory.bios = cleanInventoryValue(values["BIOS"])
		inventory.productKey = windowsProductKey()
	case "darwin":
		values := darwinHardwareValues()
		inventory.manufacturer = "Apple"
		inventory.model = cleanInventoryValue(strings.TrimSpace(strings.Join(nonEmptyStrings(values["Model Name"], values["Model Identifier"]), " ")))
		inventory.serialNumber = cleanInventoryValue(values["Serial Number (system)"])
		inventory.bios = cleanInventoryValue(values["Boot ROM Version"])
	case "linux":
		inventory.manufacturer = cleanInventoryValue(readDMI("sys_vendor"))
		inventory.model = cleanInventoryValue(readDMI("product_name"))
		inventory.serialNumber = cleanInventoryValue(readDMI("product_serial"))
		inventory.bios = cleanInventoryValue(strings.TrimSpace(strings.Join(nonEmptyStrings(
			readDMI("bios_vendor"),
			readDMI("bios_version"),
			readDMI("bios_date"),
		), " ")))
	}

	return inventory
}

func windowsInventoryValues() map[string]string {
	out, err := powershell(`
$cs = Get-CimInstance Win32_ComputerSystem | Select-Object -First 1
$bios = Get-CimInstance Win32_BIOS | Select-Object -First 1
"Manufacturer: $($cs.Manufacturer)"
"Model: $($cs.Model)"
"SerialNumber: $($bios.SerialNumber)"
"BIOS: $($bios.Manufacturer) $($bios.SMBIOSBIOSVersion) $($bios.ReleaseDate)"
`)
	if err != nil {
		return map[string]string{}
	}
	return parseKeyValueLines(out)
}

func windowsProductKey() string {
	out, err := powershell(`$key = (Get-CimInstance SoftwareLicensingService).OA3xOriginalProductKey; if ([string]::IsNullOrWhiteSpace($key)) { "N/A" } else { $key }`)
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(out), "N/A")
}

func tpmStatus() string {
	switch runtime.GOOS {
	case "linux":
		return linuxTPMStatus()
	case "windows":
		return windowsTPMStatus()
	default:
		return "Not detected"
	}
}

func linuxTPMStatus() string {
	dirs, err := filepath.Glob("/sys/class/tpm/tpm[0-9]*")
	if err != nil || len(dirs) == 0 {
		return "Not detected"
	}

	var versions []string
	enabled := fileExists("/dev/tpm0") || fileExists("/dev/tpmrm0")
	if fileExists("/dev/tpm0") || fileExists("/dev/tpmrm0") {
		enabled = true
	}

	for _, dir := range dirs {
		switch strings.TrimSpace(readFile(filepath.Join(dir, "tpm_version_major"))) {
		case "1":
			versions = append(versions, "1.2")
		case "2":
			versions = append(versions, "2.0")
		}
	}
	if len(versions) == 0 {
		versions = append(versions, "Unknown")
	}

	return formatTPMStatus(enabled, strings.Join(uniqueStrings(versions), ", "))
}

func windowsTPMStatus() string {
	out, err := powershell(`
$tpm = Get-Tpm -ErrorAction SilentlyContinue
if (-not $tpm -or -not $tpm.TpmPresent) {
  "Not detected"
} else {
  $state = if ($tpm.TpmReady -or $tpm.TpmEnabled -or $tpm.TpmActivated) { "Enabled" } else { "Disabled" }
  $spec = if ([string]::IsNullOrWhiteSpace($tpm.SpecVersion)) { "Unknown" } else { $tpm.SpecVersion }
  "$state, Version $spec"
}
`)
	if err != nil {
		return "Not detected"
	}
	return firstNonEmpty(strings.TrimSpace(out), "Not detected")
}

func formatTPMStatus(enabled bool, version string) string {
	state := "Disabled"
	if enabled {
		state = "Enabled"
	}
	return fmt.Sprintf("%s, Version %s", state, firstNonEmpty(version, "Unknown"))
}

func domainName() string {
	switch runtime.GOOS {
	case "linux":
		return linuxDomainName()
	case "darwin":
		return darwinDomainName()
	case "windows":
		return windowsDomainName()
	default:
		return "N/A"
	}
}

func linuxDomainName() string {
	if value := realmDomainName(); value != "" {
		return value
	}
	if value := krb5DefaultRealm(); value != "" {
		return value
	}
	if value := sssdDomainName(); value != "" {
		return value
	}
	if out, err := commandOutput(2*time.Second, "hostname", "-d"); err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out)
	}
	if value := resolvConfDomain(); value != "" {
		return value
	}
	return "N/A"
}

func realmDomainName() string {
	if _, err := exec.LookPath("realm"); err != nil {
		return ""
	}
	out, err := commandOutput(3*time.Second, "realm", "list", "--name-only")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				return trimmed
			}
		}
	}

	out, err = commandOutput(3*time.Second, "realm", "list")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.Contains(line, ":") {
			return line
		}
	}
	return ""
}

func krb5DefaultRealm() string {
	for _, path := range []string{"/etc/krb5.conf", "/etc/krb5/krb5.conf"} {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "default_realm") {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func sssdDomainName() string {
	file, err := os.Open("/etc/sssd/sssd.conf")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "domains") {
			parts := strings.Split(value, ",")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
	}
	return ""
}

func resolvConfDomain() string {
	file, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "domain":
			return fields[1]
		case "search":
			return fields[1]
		}
	}
	return ""
}

func darwinDomainName() string {
	out, err := commandOutput(3*time.Second, "dsconfigad", "-show")
	if err == nil {
		values := parseKeyValueLines(out)
		if value := firstNonEmpty(values["Active Directory Domain"], values["Computer Account"]); value != "" {
			return value
		}
	}
	if value := resolvConfDomain(); value != "" {
		return value
	}
	return "N/A"
}

func windowsDomainName() string {
	out, err := powershell(`$cs = Get-CimInstance Win32_ComputerSystem; if ($cs.PartOfDomain) { $cs.Domain } else { "N/A" }`)
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(out), "N/A")
}

func timezoneString() string {
	now := time.Now()
	name, offset := now.Zone()
	location := localTimezoneName()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}

	hours := offset / 3600
	minutes := (offset % 3600) / 60
	return fmt.Sprintf("%s (%s, UTC%s%02d:%02d)", location, name, sign, hours, minutes)
}

func localTimezoneName() string {
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" {
		return tz
	}
	if value := strings.TrimSpace(readFile("/etc/timezone")); value != "" {
		return value
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		link = filepath.ToSlash(link)
		if _, zone, ok := strings.Cut(link, "/zoneinfo/"); ok {
			return zone
		}
	}
	return time.Now().Location().String()
}

func darwinHardwareValues() map[string]string {
	out, err := commandOutput(8*time.Second, "system_profiler", "SPHardwareDataType")
	if err != nil {
		return map[string]string{}
	}
	return parseKeyValueLines(out)
}

func osDescription() string {
	switch runtime.GOOS {
	case "linux":
		values := parseOSRelease()
		pretty := firstNonEmpty(values["PRETTY_NAME"], values["NAME"], "Linux")
		if kernel, err := commandOutput(2*time.Second, "uname", "-r"); err == nil {
			return fmt.Sprintf("%s kernel %s %s", pretty, kernel, runtime.GOARCH)
		}
		return pretty
	case "darwin":
		name, _ := commandOutput(2*time.Second, "sw_vers", "-productName")
		version, _ := commandOutput(2*time.Second, "sw_vers", "-productVersion")
		build, _ := commandOutput(2*time.Second, "sw_vers", "-buildVersion")
		return strings.Join(nonEmptyStrings(name, version, build, runtime.GOARCH), " ")
	case "windows":
		out, err := powershell(`$os = Get-CimInstance Win32_OperatingSystem; "$($os.Caption) $($os.Version) $($os.OSArchitecture)"`)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), runtime.GOOS)
		}
	}
	return runtime.GOOS + " " + runtime.GOARCH
}

func parseOSRelease() map[string]string {
	values := map[string]string{}
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return values
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		values[key] = strings.Trim(value, `"`)
	}
	return values
}

func bootTimeString() string {
	switch runtime.GOOS {
	case "linux":
		if boot, ok := linuxBootTime(); ok {
			return boot.Format(time.RFC1123)
		}
	case "darwin":
		if boot, ok := darwinBootTime(); ok {
			return boot.Format(time.RFC1123)
		}
	case "windows":
		out, err := powershell(`$b = (Get-CimInstance Win32_OperatingSystem).LastBootUpTime; if ($b) { $b.ToString("R") } else { "N/A" }`)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	}
	return "N/A"
}

func uptimeString() string {
	switch runtime.GOOS {
	case "linux":
		fields := strings.Fields(readFile("/proc/uptime"))
		if len(fields) > 0 {
			seconds, ok := parseFloat(fields[0])
			if ok {
				return formatDuration(time.Duration(seconds * float64(time.Second)))
			}
		}
	case "darwin":
		if boot, ok := darwinBootTime(); ok {
			return formatDuration(time.Since(boot))
		}
	case "windows":
		out, err := powershell(`$u = (Get-Date) - (Get-CimInstance Win32_OperatingSystem).LastBootUpTime; "{0}d {1:00}h {2:00}m {3:00}s" -f [int]$u.TotalDays,$u.Hours,$u.Minutes,$u.Seconds`)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	}
	return "N/A"
}

func linuxBootTime() (time.Time, bool) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "btime" {
			seconds, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return time.Unix(seconds, 0), true
			}
		}
	}
	return time.Time{}, false
}

func darwinBootTime() (time.Time, bool) {
	out, err := commandOutput(2*time.Second, "sysctl", "-n", "kern.boottime")
	if err != nil {
		return time.Time{}, false
	}

	for _, part := range strings.Split(out, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "sec =") {
			fields := strings.Fields(part)
			if len(fields) >= 3 {
				seconds, err := strconv.ParseInt(fields[2], 10, 64)
				if err == nil {
					return time.Unix(seconds, 0), true
				}
			}
		}
	}
	return time.Time{}, false
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(name, "N/A")
}

func currentUserIdleTime() string {
	switch runtime.GOOS {
	case "windows":
		out, err := powershell(`
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class IdleTime {
    [StructLayout(LayoutKind.Sequential)]
    public struct LASTINPUTINFO {
        public uint cbSize;
        public uint dwTime;
    }
    [DllImport("user32.dll")]
    public static extern bool GetLastInputInfo(ref LASTINPUTINFO plii);
    public static uint GetIdleMilliseconds() {
        LASTINPUTINFO info = new LASTINPUTINFO();
        info.cbSize = (uint)Marshal.SizeOf(info);
        GetLastInputInfo(ref info);
        return ((uint)Environment.TickCount - info.dwTime);
    }
}
"@
[IdleTime]::GetIdleMilliseconds()
`)
		if err == nil {
			if ms, parseErr := strconv.ParseInt(strings.TrimSpace(out), 10, 64); parseErr == nil {
				return formatDuration(time.Duration(ms) * time.Millisecond)
			}
		}
	case "darwin":
		out, err := commandOutput(5*time.Second, "ioreg", "-c", "IOHIDSystem")
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				if !strings.Contains(line, "HIDIdleTime") {
					continue
				}
				_, value, ok := strings.Cut(line, "=")
				if !ok {
					continue
				}
				ns, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
				if parseErr == nil {
					return formatDuration(time.Duration(ns))
				}
			}
		}
	case "linux":
		if idle := linuxLoginctlInactiveTime(); idle != "" {
			return idle
		}
		if _, err := exec.LookPath("xprintidle"); err == nil {
			out, cmdErr := commandOutput(3*time.Second, "xprintidle")
			if cmdErr == nil {
				if ms, parseErr := strconv.ParseInt(strings.TrimSpace(out), 10, 64); parseErr == nil {
					return formatDuration(time.Duration(ms) * time.Millisecond)
				}
			}
		}
	}

	return "N/A"
}

func linuxLoginctlInactiveTime() string {
	if !commandExists("loginctl") {
		return ""
	}
	out, err := commandOutput(3*time.Second, "loginctl", "list-sessions", "--no-legend")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		props, propErr := commandOutput(3*time.Second, "loginctl", "show-session", fields[0], "-p", "Active", "-p", "IdleHint", "-p", "IdleSinceHint")
		if propErr != nil {
			continue
		}
		values := parseKeyValueLines(strings.ReplaceAll(props, "=", ":"))
		if values["Active"] != "yes" {
			continue
		}
		if values["IdleHint"] == "no" {
			return formatDuration(0)
		}
		if idleSince := parseUint(values["IdleSinceHint"]); idleSince > 0 {
			idleAt := time.Unix(0, int64(idleSince)*1000)
			return formatDuration(time.Since(idleAt))
		}
	}
	return ""
}

func cpuIdleSinceBoot() string {
	times, ok := readCPUTimes()
	if !ok {
		return "N/A"
	}

	return formatDuration(time.Duration((times.idle + times.iowait) * float64(time.Second/100)))
}

func lastLogin() string {
	switch runtime.GOOS {
	case "windows":
		out, err := powershell(`
$profile = Get-CimInstance Win32_NetworkLoginProfile |
  Where-Object { $_.LastLogon } |
  Sort-Object LastLogon -Descending |
  Select-Object -First 1
if ($profile) {
  if ($profile.Name -like "*\\*") { $profile.Name } else { "$env:USERDOMAIN\$($profile.Name)" }
} else {
  "N/A"
}
`)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	case "linux", "darwin":
		args := []string{"-n", "1", "-F"}
		if runtime.GOOS == "linux" {
			args = append([]string{"-w"}, args...)
		}
		out, err := commandOutput(5*time.Second, "last", args...)
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.Contains(strings.ToLower(line), "wtmp begins") {
					return qualifyAccountName(strings.Fields(line)[0])
				}
			}
		}
	}

	return "N/A"
}

func qualifyAccountName(username string) string {
	username = strings.TrimSpace(username)
	if username == "" || strings.Contains(username, `\`) || strings.Contains(username, "@") {
		return firstNonEmpty(username, "N/A")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return username
	}
	if localUserExists(username) {
		return username
	}

	realm := firstNonEmpty(krb5DefaultRealm(), domainName())
	if realm == "" || realm == "N/A" {
		return username
	}
	return strings.ToUpper(realm) + `\` + username
}

func localUserExists(username string) bool {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	prefix := username + ":"
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), prefix) {
			return true
		}
	}
	return false
}
