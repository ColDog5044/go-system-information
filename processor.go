package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func printProcessor(samples sampleData) {
	section("Processor")

	inventory := getCPUInventory()
	printKV(2, "Model", inventory.model)
	printKV(2, "Vendor", inventory.vendor)
	printKV(2, "Utilization %", formatPercent(samples.cpuUtilization))
	printKV(2, "Clock speed", inventory.clockSpeed)
	printKV(2, "Max speed", inventory.maxSpeed)
	printKV(2, "Processes", processCount())
	printKV(2, "Threads", threadCount())
	if runtime.GOOS == "windows" {
		printKV(2, "Handles", handleCount())
	}
	printKV(2, "Processors", inventory.processors)
	printKV(2, "Physical cores", inventory.physicalCores)
	printKV(2, "Logical cores", inventory.logicalCores)
	printKV(2, "External speed", inventory.externalSpeed)
	printKV(2, "L1 cache", inventory.l1Cache)
	printKV(2, "L2 cache", inventory.l2Cache)
	printKV(2, "L3 cache", inventory.l3Cache)
	printKV(2, "Architecture", inventory.architecture)
}

func readCPUTimes() (cpuTimes, bool) {
	if runtime.GOOS != "linux" {
		return cpuTimes{}, false
	}

	file, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return cpuTimes{}, false
	}

	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, false
	}

	values := make([]float64, 10)
	for i := 1; i < len(fields) && i <= 10; i++ {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			value = 0
		}
		values[i-1] = value
	}

	return cpuTimes{
		user:      values[0],
		nice:      values[1],
		system:    values[2],
		idle:      values[3],
		iowait:    values[4],
		irq:       values[5],
		softirq:   values[6],
		steal:     values[7],
		guest:     values[8],
		guestNice: values[9],
	}, true
}

func (t cpuTimes) total() float64 {
	return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal + t.guest + t.guestNice
}

func calculateCPUUtilization(before, after cpuTimes) float64 {
	total := after.total() - before.total()
	if total <= 0 {
		return math.NaN()
	}

	idle := (after.idle + after.iowait) - (before.idle + before.iowait)
	return clampPercent((total - idle) / total * 100)
}

func commandCPUUtilization() (float64, bool) {
	switch runtime.GOOS {
	case "windows":
		out, err := powershell(`$v = (Get-Counter '\Processor(_Total)\% Processor Time').CounterSamples.CookedValue; [math]::Round($v, 2)`)
		if err == nil {
			return parseFloat(strings.TrimSpace(out))
		}
	case "darwin":
		out, err := commandOutput(5*time.Second, "top", "-l", "2", "-n", "0", "-s", "1")
		if err == nil {
			var lastCPU string
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "CPU usage:") {
					lastCPU = line
				}
			}
			if lastCPU != "" {
				for _, part := range strings.Split(lastCPU, ",") {
					part = strings.TrimSpace(part)
					if strings.Contains(part, "idle") {
						fields := strings.Fields(part)
						if len(fields) > 0 {
							idle, ok := parseFloat(strings.TrimSuffix(fields[0], "%"))
							if ok {
								return clampPercent(100 - idle), true
							}
						}
					}
				}
			}
		}
	}

	return math.NaN(), false
}

func getCPUInventory() cpuInventory {
	inventory := cpuInventory{
		model:         "N/A",
		vendor:        "N/A",
		clockSpeed:    "N/A",
		maxSpeed:      "N/A",
		processors:    "N/A",
		physicalCores: "N/A",
		logicalCores:  strconv.Itoa(runtime.NumCPU()),
		externalSpeed: "N/A",
		l1Cache:       "N/A",
		l2Cache:       "N/A",
		l3Cache:       "N/A",
		architecture:  runtime.GOARCH,
	}

	switch runtime.GOOS {
	case "linux":
		linuxCPUInventory(&inventory)
	case "darwin":
		darwinCPUInventory(&inventory)
	case "windows":
		windowsCPUInventory(&inventory)
	}

	return inventory
}

func linuxCPUInventory(inventory *cpuInventory) {
	blocks := parseProcCPUInfo()
	if len(blocks) > 0 {
		first := blocks[0]
		inventory.model = firstNonEmpty(first["model name"], first["hardware"], first["processor"], "N/A")
		inventory.vendor = firstNonEmpty(first["vendor_id"], first["cpu implementer"], "N/A")
		if mhz, ok := parseFloat(first["cpu mhz"]); ok {
			inventory.clockSpeed = formatMHz(mhz)
		}
	}

	physicalIDs := map[string]bool{}
	coreIDs := map[string]bool{}
	cpuCoresPerSocket := 0
	for _, block := range blocks {
		physicalID := firstNonEmpty(block["physical id"], "0")
		if value := block["physical id"]; value != "" {
			physicalIDs[value] = true
		}
		if coreID := block["core id"]; coreID != "" {
			coreIDs[physicalID+":"+coreID] = true
		}
		if cores := parseInt(block["cpu cores"]); cores > cpuCoresPerSocket {
			cpuCoresPerSocket = cores
		}
	}

	if len(physicalIDs) > 0 {
		inventory.processors = strconv.Itoa(len(physicalIDs))
	} else if len(blocks) > 0 {
		inventory.processors = "1"
	}

	if len(coreIDs) > 0 {
		inventory.physicalCores = strconv.Itoa(len(coreIDs))
	} else if cpuCoresPerSocket > 0 {
		sockets := 1
		if len(physicalIDs) > 0 {
			sockets = len(physicalIDs)
		}
		inventory.physicalCores = strconv.Itoa(cpuCoresPerSocket * sockets)
	}

	if len(blocks) > 0 {
		inventory.logicalCores = strconv.Itoa(len(blocks))
	}

	inventory.maxSpeed = linuxMaxClockSpeed()
	caches := linuxCPUCaches()
	inventory.l1Cache = firstNonEmpty(caches["L1"], "N/A")
	inventory.l2Cache = firstNonEmpty(caches["L2"], "N/A")
	inventory.l3Cache = firstNonEmpty(caches["L3"], "N/A")
}

func parseProcCPUInfo() []map[string]string {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return nil
	}
	defer file.Close()

	var blocks []map[string]string
	current := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = map[string]string{}
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			current[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		}
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}
	return blocks
}

func darwinCPUInventory(inventory *cpuInventory) {
	if out, err := commandOutput(3*time.Second, "sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
		inventory.model = firstNonEmpty(out, "N/A")
	}
	if out, err := commandOutput(3*time.Second, "sysctl", "-n", "machdep.cpu.vendor"); err == nil {
		inventory.vendor = firstNonEmpty(out, "N/A")
	}
	if out, err := commandOutput(3*time.Second, "sysctl", "-n", "hw.cpufrequency"); err == nil {
		if hz, ok := parseFloat(out); ok {
			inventory.clockSpeed = formatMHz(hz / 1_000_000)
		}
	}
	if out, err := commandOutput(3*time.Second, "sysctl", "-n", "hw.cpufrequency_max"); err == nil {
		if hz, ok := parseFloat(out); ok {
			inventory.maxSpeed = formatMHz(hz / 1_000_000)
		}
	}
	if out, err := commandOutput(3*time.Second, "sysctl", "-n", "hw.physicalcpu"); err == nil {
		inventory.physicalCores = firstNonEmpty(strings.TrimSpace(out), "N/A")
	}
	if out, err := commandOutput(3*time.Second, "sysctl", "-n", "hw.logicalcpu"); err == nil {
		inventory.logicalCores = firstNonEmpty(strings.TrimSpace(out), "N/A")
	}
	inventory.processors = "1"

	caches := darwinCPUCaches()
	inventory.l1Cache = firstNonEmpty(caches["L1"], "N/A")
	inventory.l2Cache = firstNonEmpty(caches["L2"], "N/A")
	inventory.l3Cache = firstNonEmpty(caches["L3"], "N/A")

	if out, err := commandOutput(3*time.Second, "sysctl", "-n", "hw.busfrequency"); err == nil {
		if hz, ok := parseFloat(out); ok && hz > 0 {
			inventory.externalSpeed = formatMHz(hz / 1_000_000)
		}
	}
}

func windowsCPUInventory(inventory *cpuInventory) {
	out, err := powershell(`
$processors = @(Get-CimInstance Win32_Processor)
$p = $processors | Select-Object -First 1
"Name: $($p.Name)"
"Manufacturer: $($p.Manufacturer)"
"CurrentClockSpeed: $($p.CurrentClockSpeed)"
"MaxClockSpeed: $($p.MaxClockSpeed)"
"NumberOfCores: $($p.NumberOfCores)"
"NumberOfLogicalProcessors: $($p.NumberOfLogicalProcessors)"
"ProcessorCount: $($processors.Count)"
"ExternalClock: $($p.ExternalClock)"
`)
	if err != nil {
		return
	}

	values := parseKeyValueLines(out)
	inventory.model = firstNonEmpty(values["Name"], "N/A")
	inventory.vendor = firstNonEmpty(values["Manufacturer"], "N/A")
	if mhz, ok := parseFloat(values["CurrentClockSpeed"]); ok {
		inventory.clockSpeed = formatMHz(mhz)
	}
	if mhz, ok := parseFloat(values["MaxClockSpeed"]); ok {
		inventory.maxSpeed = formatMHz(mhz)
	}
	inventory.physicalCores = firstNonEmpty(values["NumberOfCores"], "N/A")
	inventory.logicalCores = firstNonEmpty(values["NumberOfLogicalProcessors"], strconv.Itoa(runtime.NumCPU()))
	inventory.processors = firstNonEmpty(values["ProcessorCount"], "N/A")
	if mhz, ok := parseFloat(values["ExternalClock"]); ok && mhz > 0 {
		inventory.externalSpeed = formatMHz(mhz)
	}

	caches := windowsCPUCaches()
	inventory.l1Cache = firstNonEmpty(caches["L1"], "N/A")
	inventory.l2Cache = firstNonEmpty(caches["L2"], "N/A")
	inventory.l3Cache = firstNonEmpty(caches["L3"], "N/A")
}

func linuxMaxClockSpeed() string {
	for _, path := range []string{
		"/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq",
		"/sys/devices/system/cpu/cpu0/cpufreq/scaling_max_freq",
	} {
		if value := readUintFile(path); value > 0 {
			return formatMHz(float64(value) / 1000)
		}
	}
	return "N/A"
}

func linuxCPUCaches() map[string]string {
	result := map[string][]string{}

	dirs, err := filepath.Glob("/sys/devices/system/cpu/cpu0/cache/index*")
	if err != nil {
		return map[string]string{}
	}

	for _, dir := range dirs {
		level := strings.TrimSpace(readFile(filepath.Join(dir, "level")))
		cacheType := strings.TrimSpace(readFile(filepath.Join(dir, "type")))
		size := normalizeCacheSize(readFile(filepath.Join(dir, "size")))
		if level == "" || size == "" {
			continue
		}

		key := "L" + level
		if cacheType != "" && !strings.EqualFold(cacheType, "Unified") {
			result[key] = append(result[key], fmt.Sprintf("%s %s", size, strings.ToLower(cacheType)))
		} else {
			result[key] = append(result[key], size)
		}
	}

	return flattenCacheMap(result)
}

func darwinCPUCaches() map[string]string {
	result := map[string]string{}
	sysctls := map[string]string{
		"hw.l1icachesize": "L1",
		"hw.l1dcachesize": "L1",
		"hw.l2cachesize":  "L2",
		"hw.l3cachesize":  "L3",
	}

	grouped := map[string][]string{}
	for key, level := range sysctls {
		out, err := commandOutput(3*time.Second, "sysctl", "-n", key)
		if err != nil {
			continue
		}
		bytes, parseErr := strconv.ParseUint(strings.TrimSpace(out), 10, 64)
		if parseErr != nil || bytes == 0 {
			continue
		}
		grouped[level] = append(grouped[level], formatBytes(bytes))
	}

	for level, values := range grouped {
		result[level] = strings.Join(uniqueStrings(values), ", ")
	}
	return result
}

func windowsCPUCaches() map[string]string {
	out, err := powershell(`Get-CimInstance Win32_CacheMemory | ForEach-Object { "L$($_.Level): $($_.InstalledSize) KB" }`)
	if err != nil {
		return map[string]string{}
	}

	grouped := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		grouped[strings.TrimSpace(key)] = append(grouped[strings.TrimSpace(key)], normalizeCacheSize(strings.TrimSpace(value)))
	}
	return flattenCacheMap(grouped)
}

func processCount() string {
	switch runtime.GOOS {
	case "linux":
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return "N/A"
		}
		count := 0
		for _, entry := range entries {
			if entry.IsDir() && isDigits(entry.Name()) {
				count++
			}
		}
		return strconv.Itoa(count)
	case "darwin":
		out, err := commandOutput(3*time.Second, "ps", "-ax", "-o", "pid=")
		if err != nil {
			return "N/A"
		}
		return strconv.Itoa(countNonEmptyLines(out))
	case "windows":
		out, err := powershell(`(Get-Process | Measure-Object).Count`)
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	default:
		return "N/A"
	}
}

func threadCount() string {
	switch runtime.GOOS {
	case "linux":
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return "N/A"
		}
		total := 0
		for _, entry := range entries {
			if !entry.IsDir() || !isDigits(entry.Name()) {
				continue
			}
			total += parseStatusNumber(filepath.Join("/proc", entry.Name(), "status"), "Threads")
		}
		if total == 0 {
			return "N/A"
		}
		return strconv.Itoa(total)
	case "darwin":
		out, err := commandOutput(3*time.Second, "ps", "-axo", "thcount=")
		if err != nil {
			return "N/A"
		}
		total := 0
		for _, line := range strings.Split(out, "\n") {
			total += parseInt(strings.TrimSpace(line))
		}
		if total == 0 {
			return "N/A"
		}
		return strconv.Itoa(total)
	case "windows":
		out, err := powershell(`$sum = 0; Get-Process | ForEach-Object { $sum += $_.Threads.Count }; $sum`)
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	default:
		return "N/A"
	}
}

func handleCount() string {
	if runtime.GOOS != "windows" {
		return "N/A (Windows handles)"
	}

	out, err := powershell(`$sum = (Get-Process | Measure-Object -Property HandleCount -Sum).Sum; if ($sum) { [int64]$sum } else { "N/A" }`)
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(out), "N/A")
}

func parseStatusNumber(path, key string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()

	prefix := key + ":"
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			return parseInt(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
		}
	}
	return 0
}
