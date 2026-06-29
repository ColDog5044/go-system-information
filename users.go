package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

const maxPrintedUsers = 30

func printUserInventory() {
	section("User & Session Inventory")

	users := collectUsers()
	privileged := privilegedUsers(users)
	active := activeSessions()

	printKV(2, "Local users", countOrNA(len(users)))
	printKV(2, "Privileged users", countOrNA(len(privileged)))
	printKV(2, "Active sessions", countOrNA(len(active)))

	var userLines []string
	for _, account := range privileged {
		userLines = append(userLines, userLine(account))
	}
	printLimitedStrings("Privileged", userLines, maxPrintedUsers)
	printLimitedStrings("Sessions", active, maxPrintedUsers)
}

func collectUsers() []userAccount {
	switch runtime.GOOS {
	case "linux":
		return linuxUsers()
	case "windows":
		return windowsUsers()
	case "darwin":
		return darwinUsers()
	default:
		return nil
	}
}

func linuxUsers() []userAccount {
	file, err := os.Open("/etc/passwd")
	if err != nil {
		return nil
	}
	defer file.Close()

	privileged := linuxPrivilegedUserMap()
	var users []userAccount
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 7 {
			continue
		}
		name := parts[0]
		uid := parts[2]
		shell := parts[6]
		if isSystemUser(uid, shell) {
			continue
		}
		users = append(users, userAccount{
			Name:       name,
			UID:        uid,
			Disabled:   linuxAccountDisabled(name),
			Privileged: privileged[name],
			Detail:     shell,
		})
	}
	sortUsers(users)
	return users
}

func linuxPrivilegedUserMap() map[string]bool {
	result := map[string]bool{"root": true}
	file, err := os.Open("/etc/group")
	if err != nil {
		return result
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 4 {
			continue
		}
		group := parts[0]
		if group != "sudo" && group != "wheel" && group != "admin" {
			continue
		}
		for _, member := range strings.Split(parts[3], ",") {
			if member = strings.TrimSpace(member); member != "" {
				result[member] = true
			}
		}
	}
	return result
}

func linuxAccountDisabled(name string) string {
	if !commandExists("passwd") {
		return "N/A"
	}
	out, err := commandOutput(3*time.Second, "passwd", "-S", name)
	if err != nil {
		return "N/A"
	}
	fields := strings.Fields(out)
	if len(fields) >= 2 {
		switch fields[1] {
		case "L", "LK":
			return "Yes"
		case "P", "PS", "NP":
			return "No"
		}
	}
	return "N/A"
}

func windowsUsers() []userAccount {
	out, err := powershell(`
$admins = Get-LocalGroupMember -Group Administrators -ErrorAction SilentlyContinue | ForEach-Object { $_.Name }
Get-LocalUser | ForEach-Object {
  $priv = if ($admins -contains $_.Name -or ($admins | Where-Object { $_ -like "*\$($_.Name)" })) { "true" } else { "false" }
  "Name: $($_.Name)"
  "UID: $($_.SID.Value)"
  "Disabled: $(-not $_.Enabled)"
  "Privileged: $priv"
  "Detail: $($_.Description)"
  ""
}
`)
	if err != nil {
		return nil
	}

	var users []userAccount
	for _, block := range splitBlocks(out) {
		values := parseKeyValueLines(block)
		if values["Name"] == "" {
			continue
		}
		users = append(users, userAccount{
			Name:       values["Name"],
			UID:        values["UID"],
			Disabled:   mapBoolString(values["Disabled"]),
			Privileged: strings.EqualFold(values["Privileged"], "true"),
			Detail:     values["Detail"],
		})
	}
	sortUsers(users)
	return users
}

func darwinUsers() []userAccount {
	out, err := commandOutput(8*time.Second, "dscl", ".", "-list", "/Users", "UniqueID")
	if err != nil {
		return nil
	}
	admins := darwinAdminUsers()
	var users []userAccount
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "_") {
			continue
		}
		uid := fields[1]
		if isSystemUser(uid, "") {
			continue
		}
		users = append(users, userAccount{Name: fields[0], UID: uid, Disabled: "N/A", Privileged: admins[fields[0]]})
	}
	sortUsers(users)
	return users
}

func darwinAdminUsers() map[string]bool {
	out, err := commandOutput(5*time.Second, "dscl", ".", "-read", "/Groups/admin", "GroupMembership")
	if err != nil {
		return map[string]bool{}
	}
	result := map[string]bool{}
	fields := strings.Fields(out)
	for _, field := range fields {
		if field != "GroupMembership:" {
			result[field] = true
		}
	}
	return result
}

func activeSessions() []string {
	switch runtime.GOOS {
	case "linux", "darwin":
		out, err := commandOutput(5*time.Second, "who")
		if err != nil {
			return nil
		}
		return nonEmptyLines(out)
	case "windows":
		out, err := commandOutput(5*time.Second, "query", "user")
		if err != nil {
			return nil
		}
		return nonEmptyLines(out)
	default:
		return nil
	}
}

func privilegedUsers(users []userAccount) []userAccount {
	var privileged []userAccount
	for _, account := range users {
		if account.Privileged {
			privileged = append(privileged, account)
		}
	}
	sortUsers(privileged)
	return privileged
}

func sortUsers(users []userAccount) {
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i].Name) < strings.ToLower(users[j].Name)
	})
}

func isSystemUser(uid, shell string) bool {
	parsed := parseInt(uid)
	if parsed == 0 {
		return false
	}
	if parsed < 1000 {
		return true
	}
	return strings.Contains(shell, "nologin") || strings.Contains(shell, "false")
}

func userLine(account userAccount) string {
	flags := []string{}
	if account.Disabled != "" && account.Disabled != "N/A" {
		flags = append(flags, "disabled="+account.Disabled)
	}
	if account.Detail != "" {
		flags = append(flags, account.Detail)
	}
	return fmt.Sprintf("%s uid=%s %s", account.Name, firstNonEmpty(account.UID, "N/A"), strings.Join(flags, " "))
}

func countOrNA(count int) string {
	if count == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", count)
}

func mapBoolString(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return "Yes"
	case "false":
		return "No"
	default:
		return firstNonEmpty(value, "N/A")
	}
}
