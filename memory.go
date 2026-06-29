package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func printMemory() {
	section("Memory")

	info := getMemoryInfo()
	printKV(2, "Usage %", info.usagePercent)
	printKV(2, "In use", info.inUse)
	printKV(2, "Available", info.available)
	printKV(2, "Committed", info.committed)
	printKV(2, "Cached", info.cached)
	printKV(2, "Paged pool", info.pagedPool)
	printKV(2, "Non-paged pool", info.nonPagedPool)
	printKV(2, "Hardware reserved", info.hardwareReserved)
	printKV(2, "Slots used", info.slotsUsed)
}

func getMemoryInfo() memoryInfo {
	info := memoryInfo{
		usagePercent:     "N/A",
		inUse:            "N/A",
		available:        "N/A",
		committed:        "N/A",
		cached:           "N/A",
		pagedPool:        "N/A",
		nonPagedPool:     "N/A",
		hardwareReserved: "N/A",
		slotsUsed:        "N/A",
	}

	switch runtime.GOOS {
	case "linux":
		linuxMemoryInfo(&info)
	case "darwin":
		darwinMemoryInfo(&info)
	case "windows":
		windowsMemoryInfo(&info)
	}

	return info
}

func linuxMemoryInfo(info *memoryInfo) {
	values := linuxMemInfo()
	total := values["MemTotal"] * 1024
	available := values["MemAvailable"] * 1024
	if available == 0 {
		available = (values["MemFree"] + values["Buffers"] + values["Cached"] + values["SReclaimable"]) * 1024
	}

	if total > 0 {
		used := total - available
		info.usagePercent = formatPercent(float64(used) / float64(total) * 100)
		info.inUse = formatBytes(used)
		info.available = formatBytes(available)
	}
	if committed := values["Committed_AS"] * 1024; committed > 0 {
		info.committed = formatBytes(committed)
	}
	if cached := (values["Cached"] + values["SReclaimable"]) * 1024; cached > 0 {
		info.cached = formatBytes(cached)
	}
	if slots := dmidecodeMemorySlotsUsed(); slots != "" {
		info.slotsUsed = slots
	}
}

func linuxMemInfo() map[string]uint64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return map[string]uint64{}
	}
	defer file.Close()

	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(strings.TrimSuffix(scanner.Text(), ":"))
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		values[key] = parseUint(fields[1])
	}
	return values
}

func darwinMemoryInfo(info *memoryInfo) {
	total := uint64(0)
	if out, err := commandOutput(3*time.Second, "sysctl", "-n", "hw.memsize"); err == nil {
		total = parseUint(out)
	}

	out, err := commandOutput(3*time.Second, "vm_stat")
	if err != nil || total == 0 {
		if slots := darwinMemorySlotsUsed(); slots != "" {
			info.slotsUsed = slots
		}
		return
	}

	pageSize := uint64(4096)
	stats := map[string]uint64{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "page size of") {
			fields := strings.Fields(line)
			for i, field := range fields {
				if field == "of" && i+1 < len(fields) {
					pageSize = parseUint(fields[i+1])
				}
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		stats[key] = parseUint(strings.Trim(strings.TrimSpace(value), "."))
	}

	freePages := stats["Pages free"] + stats["Pages inactive"] + stats["Pages speculative"]
	available := freePages * pageSize
	if available > total {
		available = total
	}
	used := total - available
	info.usagePercent = formatPercent(float64(used) / float64(total) * 100)
	info.inUse = formatBytes(used)
	info.available = formatBytes(available)
	if cached := stats["File-backed pages"] * pageSize; cached > 0 {
		info.cached = formatBytes(cached)
	}
	if slots := darwinMemorySlotsUsed(); slots != "" {
		info.slotsUsed = slots
	}
}

func windowsMemoryInfo(info *memoryInfo) {
	out, err := powershell(`
$os = Get-CimInstance Win32_OperatingSystem
$perf = Get-CimInstance Win32_PerfRawData_PerfOS_Memory
$installed = (Get-CimInstance Win32_PhysicalMemory | Measure-Object -Property Capacity -Sum).Sum
$slots = (Get-CimInstance Win32_PhysicalMemory | Measure-Object).Count
"TotalBytes: $([uint64]$os.TotalVisibleMemorySize * 1024)"
"AvailableBytes: $([uint64]$os.FreePhysicalMemory * 1024)"
"CommittedBytes: $($perf.CommittedBytes)"
"CacheBytes: $($perf.CacheBytes)"
"PoolPagedBytes: $($perf.PoolPagedBytes)"
"PoolNonpagedBytes: $($perf.PoolNonpagedBytes)"
"InstalledBytes: $installed"
"SlotsUsed: $slots"
`)
	if err != nil {
		return
	}

	values := parseKeyValueLines(out)
	total := parseUint(values["TotalBytes"])
	available := parseUint(values["AvailableBytes"])
	if total > 0 {
		used := total - available
		info.usagePercent = formatPercent(float64(used) / float64(total) * 100)
		info.inUse = formatBytes(used)
		info.available = formatBytes(available)
	}
	if committed := parseUint(values["CommittedBytes"]); committed > 0 {
		info.committed = formatBytes(committed)
	}
	if cached := parseUint(values["CacheBytes"]); cached > 0 {
		info.cached = formatBytes(cached)
	}
	if paged := parseUint(values["PoolPagedBytes"]); paged > 0 {
		info.pagedPool = formatBytes(paged)
	}
	if nonPaged := parseUint(values["PoolNonpagedBytes"]); nonPaged > 0 {
		info.nonPagedPool = formatBytes(nonPaged)
	}
	if installed := parseUint(values["InstalledBytes"]); installed > total && total > 0 {
		info.hardwareReserved = formatBytes(installed - total)
	}
	info.slotsUsed = firstNonEmpty(values["SlotsUsed"], "N/A")
}

func dmidecodeMemorySlotsUsed() string {
	if _, err := exec.LookPath("dmidecode"); err != nil {
		return ""
	}

	out, err := commandOutput(5*time.Second, "dmidecode", "-t", "memory")
	if err != nil {
		return ""
	}

	totalSlots := 0
	usedSlots := 0
	inMemoryDevice := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Memory Device") {
			inMemoryDevice = true
			totalSlots++
			continue
		}
		if !inMemoryDevice || !strings.HasPrefix(trimmed, "Size:") {
			continue
		}

		size := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "Size:")))
		if size != "" && !strings.Contains(size, "no module installed") && !strings.Contains(size, "unknown") {
			usedSlots++
		}
		inMemoryDevice = false
	}

	if totalSlots == 0 {
		return ""
	}
	return fmt.Sprintf("%d / %d", usedSlots, totalSlots)
}

func darwinMemorySlotsUsed() string {
	out, err := commandOutput(8*time.Second, "system_profiler", "SPMemoryDataType")
	if err != nil {
		return ""
	}

	used := strings.Count(out, "Status: OK")
	if used == 0 {
		return ""
	}
	return strconv.Itoa(used)
}
