package main

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"
)

const maxPrintedServices = 20

func printServices() {
	section("Services")

	services := collectServices()
	if len(services) == 0 {
		printKV(2, "Services", "N/A")
		return
	}

	running := 0
	failed := 0
	enabled := 0
	var problemServices []string
	for _, svc := range services {
		state := strings.ToLower(svc.State)
		startType := strings.ToLower(svc.StartType)
		if state == "running" || state == "active" {
			running++
		}
		if state == "failed" || strings.Contains(state, "stopped") && strings.Contains(strings.ToLower(svc.Detail), "failed") {
			failed++
			problemServices = append(problemServices, serviceLine(svc))
		}
		if startType == "enabled" || startType == "auto" || startType == "automatic" {
			enabled++
		}
	}

	printKV(2, "Total", fmt.Sprintf("%d", len(services)))
	printKV(2, "Running", fmt.Sprintf("%d", running))
	printKV(2, "Enabled/automatic", fmt.Sprintf("%d", enabled))
	printKV(2, "Failed/problem", fmt.Sprintf("%d", failed))

	if len(problemServices) > 0 {
		printLimitedStrings("Problem services", problemServices, maxPrintedServices)
	}
}

func collectServices() []serviceInfo {
	switch runtime.GOOS {
	case "linux":
		return linuxServices()
	case "windows":
		return windowsServices()
	case "darwin":
		return darwinServices()
	default:
		return nil
	}
}

func linuxServices() []serviceInfo {
	if !commandExists("systemctl") {
		return nil
	}

	enabledMap := linuxServiceEnabledMap()
	out, err := commandOutput(10*time.Second, "systemctl", "list-units", "--type=service", "--all", "--no-legend", "--plain")
	if err != nil {
		return nil
	}

	var services []serviceInfo
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := strings.TrimPrefix(fields[0], "●")
		name = strings.TrimSpace(name)
		description := ""
		if len(fields) > 4 {
			description = strings.Join(fields[4:], " ")
		}
		services = append(services, serviceInfo{
			Name:        name,
			DisplayName: description,
			State:       fields[2],
			StartType:   firstNonEmpty(enabledMap[name], "N/A"),
			Detail:      fields[3],
		})
	}
	sortServices(services)
	return services
}

func linuxServiceEnabledMap() map[string]string {
	out, err := commandOutput(10*time.Second, "systemctl", "list-unit-files", "--type=service", "--no-legend", "--plain")
	if err != nil {
		return map[string]string{}
	}

	result := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			result[fields[0]] = fields[1]
		}
	}
	return result
}

func windowsServices() []serviceInfo {
	out, err := powershell(`
Get-CimInstance Win32_Service | ForEach-Object {
  "Name: $($_.Name)"
  "DisplayName: $($_.DisplayName)"
  "State: $($_.State)"
  "StartType: $($_.StartMode)"
  "Detail: $($_.Status)"
  ""
}
`)
	if err != nil {
		return nil
	}

	var services []serviceInfo
	for _, block := range splitBlocks(out) {
		values := parseKeyValueLines(block)
		if values["Name"] == "" {
			continue
		}
		services = append(services, serviceInfo{
			Name:        values["Name"],
			DisplayName: values["DisplayName"],
			State:       values["State"],
			StartType:   values["StartType"],
			Detail:      values["Detail"],
		})
	}
	sortServices(services)
	return services
}

func darwinServices() []serviceInfo {
	if !commandExists("launchctl") {
		return nil
	}
	out, err := commandOutput(8*time.Second, "launchctl", "list")
	if err != nil {
		return nil
	}

	var services []serviceInfo
	for i, line := range strings.Split(out, "\n") {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		state := "stopped"
		if fields[0] != "-" {
			state = "running"
		}
		services = append(services, serviceInfo{
			Name:      fields[2],
			State:     state,
			StartType: "launchd",
			Detail:    "status " + fields[1],
		})
	}
	sortServices(services)
	return services
}

func sortServices(services []serviceInfo) {
	sort.Slice(services, func(i, j int) bool {
		return strings.ToLower(services[i].Name) < strings.ToLower(services[j].Name)
	})
}

func serviceLine(svc serviceInfo) string {
	name := firstNonEmpty(svc.DisplayName, svc.Name)
	return fmt.Sprintf("%s (%s, %s)", truncateString(name, 64), firstNonEmpty(svc.State, "N/A"), firstNonEmpty(svc.StartType, "N/A"))
}
