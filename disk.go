package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func printDisks(samples sampleData) {
	section("Disk Volumes")

	partitions := listPartitions()
	if len(partitions) == 0 {
		printKV(2, "Volumes", "N/A")
		return
	}

	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i].Mountpoint < partitions[j].Mountpoint
	})

	seen := map[string]bool{}
	for _, partition := range partitions {
		if partition.Mountpoint == "" || seen[partition.Mountpoint] {
			continue
		}
		seen[partition.Mountpoint] = true

		usage, usageOK := diskUsage(partition)
		rate, rateOK := matchDiskRate(partition, samples.diskRates)

		group(partition.Mountpoint)
		printKV(4, "Device", firstNonEmpty(partition.Device, "N/A"))
		printKV(4, "Volume name", volumeName(partition))
		printKV(4, "Active time", formatOptionalPercent(rate.activePercent, rateOK))
		printKV(4, "Disk transfer rate", formatOptionalBytesPerSecond(rate.transferSec, rateOK))
		if usageOK {
			printKV(4, "Disk usage %", formatPercent(usage.UsedPercent))
			printKV(4, "Disk usage", fmt.Sprintf("%s / %s", formatBytes(usage.Used), formatBytes(usage.Total)))
			printKV(4, "File system", firstNonEmpty(usage.FSType, partition.FSType, "N/A"))
			printKV(4, "Max file name length", maxFileNameLength(partition.Mountpoint, firstNonEmpty(usage.FSType, partition.FSType)))
		} else {
			printKV(4, "Disk usage %", "N/A")
			printKV(4, "Disk usage", "N/A")
			printKV(4, "File system", firstNonEmpty(partition.FSType, "N/A"))
			printKV(4, "Max file name length", "N/A")
		}
		printKV(4, "Average read speed", formatOptionalBytesPerSecond(rate.readBytesSec, rateOK))
		printKV(4, "Response time", formatOptionalMilliseconds(rate.responseMS, rateOK))
		printKV(4, "Average write speed", formatOptionalBytesPerSecond(rate.writeBytesSec, rateOK))
		printKV(4, "Drive type", driveType(partition))
		if runtime.GOOS == "windows" {
			printKV(4, "BitLocker status", bitLockerStatus(partition))
		}
		printKV(4, "Auto mount", autoMountStatus(partition))
		printKV(4, "Compressed", compressedStatus(partition))
		printKV(4, "Page file", pageFileStatus(partition))
		if runtime.GOOS == "windows" {
			printKV(4, "Index", indexStatus(partition))
		}
	}
}

func readDiskCounters() map[string]diskCounter {
	if runtime.GOOS != "linux" {
		return map[string]diskCounter{}
	}

	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return map[string]diskCounter{}
	}
	defer file.Close()

	counters := map[string]diskCounter{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}

		name := fields[2]
		readOps := parseUint(fields[3])
		sectorsRead := parseUint(fields[5])
		readTime := parseUint(fields[6])
		writeOps := parseUint(fields[7])
		sectorsWritten := parseUint(fields[9])
		writeTime := parseUint(fields[10])
		ioTime := parseUint(fields[12])
		sectorSize := blockSectorSize(name)

		counters[name] = diskCounter{
			name:        name,
			readOps:     readOps,
			writeOps:    writeOps,
			readBytes:   sectorsRead * sectorSize,
			writeBytes:  sectorsWritten * sectorSize,
			readTimeMS:  readTime,
			writeTimeMS: writeTime,
			ioTimeMS:    ioTime,
		}
	}

	return counters
}

func calculateDiskRates(before, after map[string]diskCounter, seconds float64) map[string]diskRate {
	rates := map[string]diskRate{}
	if seconds <= 0 {
		return rates
	}

	for name, end := range after {
		start, ok := before[name]
		if !ok {
			continue
		}

		readBytes := deltaUint64(start.readBytes, end.readBytes)
		writeBytes := deltaUint64(start.writeBytes, end.writeBytes)
		readOps := deltaUint64(start.readOps, end.readOps)
		writeOps := deltaUint64(start.writeOps, end.writeOps)
		readTime := deltaUint64(start.readTimeMS, end.readTimeMS)
		writeTime := deltaUint64(start.writeTimeMS, end.writeTimeMS)
		ioTime := deltaUint64(start.ioTimeMS, end.ioTimeMS)

		totalOps := readOps + writeOps
		responseMS := math.NaN()
		if totalOps > 0 {
			responseMS = float64(readTime+writeTime) / float64(totalOps)
		}

		rates[name] = diskRate{
			activePercent: clampPercent(float64(ioTime) / (seconds * 1000) * 100),
			readBytesSec:  float64(readBytes) / seconds,
			writeBytesSec: float64(writeBytes) / seconds,
			transferSec:   float64(readBytes+writeBytes) / seconds,
			responseMS:    responseMS,
		}
	}

	return rates
}

func listPartitions() []partitionStat {
	switch runtime.GOOS {
	case "linux":
		return linuxPartitions()
	case "darwin":
		return darwinPartitions()
	case "windows":
		return windowsPartitions()
	default:
		return nil
	}
}

func linuxPartitions() []partitionStat {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	defer file.Close()

	var partitions []partitionStat
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}

		dash := -1
		for i, field := range fields {
			if field == "-" {
				dash = i
				break
			}
		}
		if dash < 0 || dash+2 >= len(fields) {
			continue
		}

		mountpoint := unescapeMountField(fields[4])
		options := strings.Split(fields[5], ",")
		fstype := fields[dash+1]
		source := unescapeMountField(fields[dash+2])
		if isPseudoFilesystem(fstype, source, mountpoint) {
			continue
		}

		partitions = append(partitions, partitionStat{
			Device:     source,
			Mountpoint: mountpoint,
			FSType:     fstype,
			Opts:       options,
		})
	}
	return partitions
}

func darwinPartitions() []partitionStat {
	out, err := commandOutput(5*time.Second, "mount")
	if err != nil {
		return nil
	}

	var partitions []partitionStat
	for _, line := range strings.Split(out, "\n") {
		device, rest, ok := strings.Cut(line, " on ")
		if !ok {
			continue
		}
		mountpoint, details, ok := strings.Cut(rest, " (")
		if !ok {
			continue
		}
		details = strings.TrimSuffix(details, ")")
		parts := strings.Split(details, ",")
		fstype := ""
		if len(parts) > 0 {
			fstype = strings.TrimSpace(parts[0])
		}
		if isPseudoFilesystem(fstype, device, mountpoint) {
			continue
		}
		partitions = append(partitions, partitionStat{
			Device:     strings.TrimSpace(device),
			Mountpoint: strings.TrimSpace(mountpoint),
			FSType:     fstype,
			Opts:       trimStrings(parts[1:]),
		})
	}
	return partitions
}

func windowsPartitions() []partitionStat {
	out, err := powershell(`
Get-CimInstance Win32_LogicalDisk | ForEach-Object {
  "Device: $($_.DeviceID)"
  "Mountpoint: $($_.DeviceID)\"
  "FSType: $($_.FileSystem)"
  "Opts:"
  ""
}
`)
	if err != nil {
		return nil
	}

	var partitions []partitionStat
	for _, block := range splitBlocks(out) {
		values := parseKeyValueLines(block)
		if values["Device"] == "" || values["Mountpoint"] == "" {
			continue
		}
		partitions = append(partitions, partitionStat{
			Device:     values["Device"],
			Mountpoint: values["Mountpoint"],
			FSType:     values["FSType"],
		})
	}
	return partitions
}

func isPseudoFilesystem(fstype, source, mountpoint string) bool {
	pseudo := map[string]bool{
		"autofs":      true,
		"binfmt_misc": true,
		"bpf":         true,
		"cgroup":      true,
		"cgroup2":     true,
		"configfs":    true,
		"debugfs":     true,
		"devfs":       true,
		"devpts":      true,
		"devtmpfs":    true,
		"fdesc":       true,
		"fusectl":     true,
		"hugetlbfs":   true,
		"mqueue":      true,
		"nsfs":        true,
		"proc":        true,
		"pstore":      true,
		"rpc_pipefs":  true,
		"securityfs":  true,
		"sysfs":       true,
		"tracefs":     true,
		"tmpfs":       true,
		"map":         true,
		"nullfs":      true,
		"unionfs":     true,
		"portal":      true,
		"synthetic":   true,
	}
	if pseudo[strings.ToLower(fstype)] {
		return true
	}
	if strings.HasPrefix(mountpoint, "/proc") || strings.HasPrefix(mountpoint, "/sys") || strings.HasPrefix(mountpoint, "/run") {
		return true
	}
	return source == "" && mountpoint == ""
}

func diskUsage(partition partitionStat) (volumeUsage, bool) {
	switch runtime.GOOS {
	case "linux", "darwin":
		out, err := commandOutput(4*time.Second, "df", "-kP", partition.Mountpoint)
		if err != nil {
			return volumeUsage{}, false
		}
		lines := nonEmptyLines(out)
		if len(lines) < 2 {
			return volumeUsage{}, false
		}
		fields := strings.Fields(lines[len(lines)-1])
		if len(fields) < 5 {
			return volumeUsage{}, false
		}
		total := parseUint(fields[1]) * 1024
		used := parseUint(fields[2]) * 1024
		free := parseUint(fields[3]) * 1024
		percent := 0.0
		if total > 0 {
			percent = float64(used) / float64(total) * 100
		} else {
			percent, _ = parseFloat(strings.TrimSuffix(fields[4], "%"))
		}
		return volumeUsage{Total: total, Used: used, Free: free, UsedPercent: percent, FSType: partition.FSType}, true
	case "windows":
		drive := windowsDriveLetter(partition)
		if drive == "" {
			return volumeUsage{}, false
		}
		out, err := powershell(fmt.Sprintf(`
$d = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='%s:'"
if ($d) {
  "Size: $($d.Size)"
  "Free: $($d.FreeSpace)"
  "FSType: $($d.FileSystem)"
}
`, drive))
		if err != nil {
			return volumeUsage{}, false
		}
		values := parseKeyValueLines(out)
		total := parseUint(values["Size"])
		free := parseUint(values["Free"])
		used := total - free
		percent := 0.0
		if total > 0 {
			percent = float64(used) / float64(total) * 100
		}
		return volumeUsage{Total: total, Used: used, Free: free, UsedPercent: percent, FSType: values["FSType"]}, total > 0
	default:
		return volumeUsage{}, false
	}
}

func matchDiskRate(partition partitionStat, rates map[string]diskRate) (diskRate, bool) {
	for _, candidate := range diskCandidates(partition) {
		if rate, ok := rates[candidate]; ok {
			return rate, true
		}
	}
	return diskRate{}, false
}

func diskCandidates(partition partitionStat) []string {
	candidates := []string{
		partition.Device,
		partition.Mountpoint,
		filepath.Base(partition.Device),
		strings.TrimPrefix(partition.Device, "/dev/"),
		strings.TrimSuffix(partition.Device, ":"),
		strings.TrimSuffix(partition.Mountpoint, `\`),
	}

	if len(partition.Mountpoint) >= 2 && partition.Mountpoint[1] == ':' {
		candidates = append(candidates, partition.Mountpoint[:2], partition.Mountpoint[:1])
	}
	if len(partition.Device) >= 2 && partition.Device[1] == ':' {
		candidates = append(candidates, partition.Device[:2], partition.Device[:1])
	}

	return uniqueStrings(candidates)
}

func volumeName(partition partitionStat) string {
	switch runtime.GOOS {
	case "linux":
		if partition.Device != "" {
			if out, err := commandOutput(3*time.Second, "lsblk", "-no", "LABEL", partition.Device); err == nil {
				return firstNonEmpty(strings.TrimSpace(out), "N/A")
			}
		}
	case "darwin":
		if out, err := commandOutput(5*time.Second, "diskutil", "info", partition.Mountpoint); err == nil {
			return firstNonEmpty(parseKeyValueLines(out)["Volume Name"], "N/A")
		}
	case "windows":
		if drive := windowsDriveLetter(partition); drive != "" {
			out, err := powershell(fmt.Sprintf(`$v = Get-Volume -DriveLetter %s -ErrorAction SilentlyContinue; if ($v) { $v.FileSystemLabel }`, psQuote(drive)))
			if err == nil {
				return firstNonEmpty(strings.TrimSpace(out), "N/A")
			}
		}
	}

	return "N/A"
}

func driveType(partition partitionStat) string {
	switch runtime.GOOS {
	case "linux":
		if partition.Device == "" {
			return "N/A"
		}
		out, err := commandOutput(3*time.Second, "lsblk", "-no", "TRAN,ROTA,TYPE", partition.Device)
		if err != nil {
			return "N/A"
		}
		fields := strings.Fields(out)
		if len(fields) < 2 {
			return "N/A"
		}

		transport := strings.ToUpper(fields[0])
		rotation := fields[1]
		kind := "SSD/flash"
		if rotation == "1" {
			kind = "HDD"
		}
		if transport != "" && transport != "-" {
			return fmt.Sprintf("%s (%s)", kind, transport)
		}
		return kind
	case "darwin":
		out, err := commandOutput(5*time.Second, "diskutil", "info", partition.Mountpoint)
		if err != nil {
			return "N/A"
		}
		values := parseKeyValueLines(out)
		solidState := values["Solid State"]
		protocol := values["Protocol"]
		if solidState != "" || protocol != "" {
			return strings.TrimSpace(strings.Join(nonEmptyStrings(mapYesNo(solidState, "SSD/flash", "HDD"), protocol), " "))
		}
	case "windows":
		if drive := windowsDriveLetter(partition); drive != "" {
			out, err := powershell(fmt.Sprintf(`$v = Get-Volume -DriveLetter %s -ErrorAction SilentlyContinue; if ($v) { $v | Get-Partition | Get-Disk | Select-Object -First 1 -ExpandProperty MediaType }`, psQuote(drive)))
			if err == nil {
				return firstNonEmpty(strings.TrimSpace(out), "N/A")
			}
		}
	}

	return "N/A"
}

func bitLockerStatus(partition partitionStat) string {
	drive := windowsDriveLetter(partition)
	if drive == "" {
		return "N/A"
	}

	out, err := powershell(fmt.Sprintf(`$v = Get-BitLockerVolume -MountPoint %s -ErrorAction SilentlyContinue; if ($v) { "$($v.ProtectionStatus)" } else { "N/A" }`, psQuote(drive+":")))
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(out), "N/A")
}

func autoMountStatus(partition partitionStat) string {
	if containsOption(partition.Opts, "noauto") {
		return "No"
	}
	if partition.Mountpoint != "" {
		return "Yes"
	}
	return "N/A"
}

func compressedStatus(partition partitionStat) string {
	for _, opt := range partition.Opts {
		if strings.HasPrefix(strings.ToLower(opt), "compress") {
			return "Yes"
		}
	}

	if runtime.GOOS == "windows" {
		return "N/A"
	}
	return "No"
}

func pageFileStatus(partition partitionStat) string {
	switch runtime.GOOS {
	case "linux":
		swaps := readLinuxSwaps()
		for _, candidate := range diskCandidates(partition) {
			if swaps[candidate] {
				return "Yes"
			}
		}
		return "No"
	case "windows":
		drive := windowsDriveLetter(partition)
		if drive == "" {
			return "N/A"
		}
		out, err := powershell(fmt.Sprintf(`$pf = Get-CimInstance Win32_PageFileUsage | Where-Object { $_.Name -like "%s*" }; if ($pf) { "Yes" } else { "No" }`, drive+":"))
		if err != nil {
			return "N/A"
		}
		return firstNonEmpty(strings.TrimSpace(out), "N/A")
	default:
		return "N/A"
	}
}

func indexStatus(partition partitionStat) string {
	if runtime.GOOS != "windows" {
		return "N/A"
	}

	drive := windowsDriveLetter(partition)
	if drive == "" {
		return "N/A"
	}

	out, err := powershell(fmt.Sprintf(`$v = Get-CimInstance Win32_Volume -Filter "DriveLetter='%s:'" -ErrorAction SilentlyContinue; if ($v -and $null -ne $v.IndexingEnabled) { if ($v.IndexingEnabled) { "Yes" } else { "No" } } else { "N/A" }`, drive))
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(out), "N/A")
}

func maxFileNameLength(mountpoint, fstype string) string {
	switch runtime.GOOS {
	case "linux":
		out, err := commandOutput(3*time.Second, "stat", "-f", "-c", "%l", mountpoint)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	case "darwin":
		out, err := commandOutput(3*time.Second, "stat", "-f", "%l", mountpoint)
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	case "windows":
		switch strings.ToLower(fstype) {
		case "ntfs", "refs", "exfat", "fat32":
			return "255"
		}
	}

	return "N/A"
}

func readLinuxSwaps() map[string]bool {
	swaps := map[string]bool{}
	file, err := os.Open("/proc/swaps")
	if err != nil {
		return swaps
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] == "Filename" {
			continue
		}
		swaps[fields[0]] = true
		swaps[filepath.Base(fields[0])] = true
	}
	return swaps
}

func blockSectorSize(name string) uint64 {
	for _, path := range []string{
		filepath.Join("/sys/class/block", name, "queue/logical_block_size"),
		filepath.Join("/sys/class/block", name, "queue/hw_sector_size"),
	} {
		if value := readUintFile(path); value > 0 {
			return value
		}
	}
	return 512
}
