package main

import (
	"fmt"
	"math"
	"time"
)

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		return "N/A"
	}

	totalSeconds := int64(duration.Seconds())
	days := totalSeconds / 86400
	totalSeconds %= 86400
	hours := totalSeconds / 3600
	totalSeconds %= 3600
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60

	if days > 0 {
		return fmt.Sprintf("%dd %02dh %02dm %02ds", days, hours, minutes, seconds)
	}
	return fmt.Sprintf("%02dh %02dm %02ds", hours, minutes, seconds)
}

func formatBytes(bytes uint64) string {
	return formatFloatBytes(float64(bytes))
}

func formatFloatBytes(bytes float64) string {
	if math.IsNaN(bytes) || bytes < 0 {
		return "N/A"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	value := bytes
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%.0f %s", value, units[unit])
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}

func formatBytesPerSecond(bytesPerSecond float64) string {
	return formatFloatBytes(bytesPerSecond) + "/s"
}

func formatBitsPerSecond(bytesPerSecond float64) string {
	bits := bytesPerSecond * 8
	if math.IsNaN(bits) || bits < 0 {
		return "N/A"
	}

	units := []string{"bps", "Kbps", "Mbps", "Gbps", "Tbps"}
	unit := 0
	for bits >= 1000 && unit < len(units)-1 {
		bits /= 1000
		unit++
	}
	return fmt.Sprintf("%.2f %s", bits, units[unit])
}

func formatMHz(mhz float64) string {
	if mhz <= 0 || math.IsNaN(mhz) {
		return "N/A"
	}
	if mhz >= 1000 {
		return fmt.Sprintf("%.2f GHz", mhz/1000)
	}
	return fmt.Sprintf("%.0f MHz", mhz)
}

func formatPercent(percent float64) string {
	if math.IsNaN(percent) {
		return "N/A"
	}
	return fmt.Sprintf("%.2f%%", clampPercent(percent))
}

func formatOptionalPercent(percent float64, ok bool) string {
	if !ok {
		return "N/A"
	}
	return formatPercent(percent)
}

func formatOptionalBytesPerSecond(bytesPerSecond float64, ok bool) string {
	if !ok {
		return "N/A"
	}
	return formatBytesPerSecond(bytesPerSecond)
}

func formatOptionalBitsPerSecond(bytesPerSecond float64, ok bool) string {
	if !ok {
		return "N/A"
	}
	return formatBitsPerSecond(bytesPerSecond)
}

func formatOptionalMilliseconds(ms float64, ok bool) string {
	if !ok || math.IsNaN(ms) {
		return "N/A"
	}
	return fmt.Sprintf("%.2f ms", ms)
}

func printKV(indent int, label, value string) {
	currentRenderer.Field(indent, label, value)
}

func section(title string) {
	currentRenderer.Section(title)
}

func group(title string) {
	currentRenderer.Group(title)
}

func printLimitedStrings(label string, values []string, limit int) {
	if len(values) == 0 {
		return
	}
	currentRenderer.List(label, values, limit)
}
