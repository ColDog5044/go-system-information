package main

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"
)

const maxPrintedSoftware = 50

func printSoftwareInventory() {
	section("Software Inventory")

	packages := collectSoftware()
	if len(packages) == 0 {
		printKV(2, "Installed software", "N/A")
		return
	}

	printKV(2, "Installed software", fmt.Sprintf("%d", len(packages)))

	var lines []string
	for _, pkg := range packages {
		lines = append(lines, softwareLine(pkg))
	}
	printLimitedStrings("Packages", lines, maxPrintedSoftware)
}

func collectSoftware() []softwarePackage {
	switch runtime.GOOS {
	case "linux":
		return linuxSoftware()
	case "windows":
		return windowsSoftware()
	case "darwin":
		return darwinSoftware()
	default:
		return nil
	}
}

func linuxSoftware() []softwarePackage {
	switch {
	case commandExists("rpm"):
		out, err := commandOutput(20*time.Second, "rpm", "-qa", "--qf", "%{NAME}|%{VERSION}-%{RELEASE}|%{VENDOR}\n")
		if err != nil {
			return nil
		}
		return parseSoftwareRows(out)
	case commandExists("dpkg-query"):
		out, err := commandOutput(20*time.Second, "dpkg-query", "-W", "-f=${Package}|${Version}|${Maintainer}\n")
		if err != nil {
			return nil
		}
		return parseSoftwareRows(out)
	case commandExists("pacman"):
		out, err := commandOutput(20*time.Second, "pacman", "-Q")
		if err != nil {
			return nil
		}
		var packages []softwarePackage
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				packages = append(packages, softwarePackage{Name: fields[0], Version: fields[1]})
			}
		}
		sortSoftware(packages)
		return packages
	default:
		return nil
	}
}

func windowsSoftware() []softwarePackage {
	out, err := powershell(`
$paths = @(
  "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*",
  "HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*"
)
Get-ItemProperty $paths -ErrorAction SilentlyContinue |
  Where-Object { $_.DisplayName } |
  ForEach-Object { "$($_.DisplayName)|$($_.DisplayVersion)|$($_.Publisher)" }
`)
	if err != nil {
		return nil
	}
	return parseSoftwareRows(out)
}

func darwinSoftware() []softwarePackage {
	out, err := commandOutput(20*time.Second, "system_profiler", "SPApplicationsDataType")
	if err != nil {
		return nil
	}

	var packages []softwarePackage
	current := softwarePackage{}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ") {
			if current.Name != "" {
				packages = append(packages, current)
			}
			current = softwarePackage{Name: strings.TrimSuffix(trimmed, ":")}
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		switch key {
		case "Version":
			current.Version = strings.TrimSpace(value)
		case "Obtained from":
			current.Publisher = strings.TrimSpace(value)
		}
	}
	if current.Name != "" {
		packages = append(packages, current)
	}
	sortSoftware(packages)
	return packages
}

func parseSoftwareRows(output string) []softwarePackage {
	var packages []softwarePackage
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		pkg := softwarePackage{Name: parts[0]}
		if len(parts) > 1 {
			pkg.Version = parts[1]
		}
		if len(parts) > 2 {
			pkg.Publisher = parts[2]
		}
		packages = append(packages, pkg)
	}
	sortSoftware(packages)
	return packages
}

func sortSoftware(packages []softwarePackage) {
	sort.Slice(packages, func(i, j int) bool {
		return strings.ToLower(packages[i].Name) < strings.ToLower(packages[j].Name)
	})
}

func softwareLine(pkg softwarePackage) string {
	parts := nonEmptyStrings(pkg.Name, pkg.Version, pkg.Publisher)
	return truncateString(strings.Join(parts, " | "), 120)
}
