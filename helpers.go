package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func commandOutput(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", ctx.Err()
	}
	if err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func powershell(script string) (string, error) {
	var lastErr error
	for _, shell := range []string{"powershell", "pwsh"} {
		out, err := commandOutput(10*time.Second, shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	return "", lastErr
}

func parseKeyValueLines(output string) map[string]string {
	result := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.Trim(key, `"`))
		value = strings.TrimSpace(strings.Trim(value, `"`))
		if key != "" {
			result[key] = value
		}
	}
	return result
}

func splitBlocks(output string) []string {
	normalized := strings.ReplaceAll(output, "\r\n", "\n")
	var blocks []string
	var current []string
	for _, line := range strings.Split(normalized, "\n") {
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				blocks = append(blocks, strings.Join(current, "\n"))
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		blocks = append(blocks, strings.Join(current, "\n"))
	}
	return blocks
}

func readDMI(name string) string {
	return readFile(filepath.Join("/sys/class/dmi/id", name))
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func readUintFile(path string) uint64 {
	value, err := strconv.ParseUint(strings.TrimSpace(readFile(path)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func cleanInventoryValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "N/A"
	}

	lower := strings.ToLower(value)
	badValues := []string{
		"none",
		"unknown",
		"not specified",
		"not available",
		"to be filled by o.e.m.",
		"to be filled by oem",
		"system serial number",
		"default string",
	}
	for _, bad := range badValues {
		if lower == bad {
			return "N/A"
		}
	}

	return value
}

func normalizeCacheSize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	fields := strings.Fields(value)
	if len(fields) == 2 {
		return strings.ToUpper(fields[0] + " " + fields[1])
	}

	last := value[len(value)-1]
	number := strings.TrimSpace(value[:len(value)-1])
	switch last {
	case 'K', 'k':
		return number + " KB"
	case 'M', 'm':
		return number + " MB"
	case 'G', 'g':
		return number + " GB"
	}

	if bytes, err := strconv.ParseUint(value, 10, 64); err == nil {
		return formatBytes(bytes)
	}

	return value
}

func flattenCacheMap(grouped map[string][]string) map[string]string {
	result := map[string]string{}
	for key, values := range grouped {
		result[key] = strings.Join(uniqueStrings(values), ", ")
	}
	return result
}

func deltaUint64(start, end uint64) uint64 {
	if end < start {
		return 0
	}
	return end - start
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonEmptyStrings(values ...string) []string {
	var result []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" && trimmed != "N/A" {
			result = append(result, trimmed)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func joinOrNA(values []string) string {
	values = uniqueStrings(values)
	if len(values) == 0 {
		return "N/A"
	}
	return strings.Join(values, ", ")
}

func containsOption(options []string, target string) bool {
	target = strings.ToLower(target)
	for _, option := range options {
		if strings.ToLower(option) == target {
			return true
		}
	}
	return false
}

func mapYesNo(value, yesValue, noValue string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true":
		return yesValue
	case "no", "false":
		return noValue
	default:
		return ""
	}
}

func windowsDriveLetter(partition partitionStat) string {
	for _, value := range []string{partition.Mountpoint, partition.Device} {
		if len(value) >= 2 && value[1] == ':' {
			return strings.ToUpper(value[:1])
		}
	}
	return ""
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func unescapeMountField(value string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(value)
}

func trimStrings(values []string) []string {
	var result []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func countNonEmptyLines(value string) int {
	return len(nonEmptyLines(value))
}

func truncateString(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseFloat(value string) (float64, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", "."))
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func parseUint(value string) uint64 {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return 0
	}
	parsed, err := strconv.ParseUint(strings.Trim(fields[0], "."), 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}
