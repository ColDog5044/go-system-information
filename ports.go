package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxPrintedPorts = 50

func printNetworkExposure() {
	section("Network Exposure")

	ports := listeningPorts()
	if len(ports) == 0 {
		printKV(2, "Listening ports", "N/A")
		return
	}

	tcp := 0
	udp := 0
	var lines []string
	for _, port := range ports {
		if strings.HasPrefix(strings.ToLower(port.Protocol), "tcp") {
			tcp++
		}
		if strings.HasPrefix(strings.ToLower(port.Protocol), "udp") {
			udp++
		}
		lines = append(lines, listeningPortLine(port))
	}

	printKV(2, "Listening ports", fmt.Sprintf("%d", len(ports)))
	printKV(2, "TCP listeners", fmt.Sprintf("%d", tcp))
	printKV(2, "UDP endpoints", fmt.Sprintf("%d", udp))
	printLimitedStrings("Ports", lines, maxPrintedPorts)
}

func listeningPorts() []listeningPort {
	switch runtime.GOOS {
	case "linux":
		return linuxListeningPorts()
	case "windows":
		return windowsListeningPorts()
	case "darwin":
		return darwinListeningPorts()
	default:
		return nil
	}
}

func linuxListeningPorts() []listeningPort {
	processes := linuxSocketProcessMap()
	var ports []listeningPort
	ports = append(ports, parseLinuxNetFile("/proc/net/tcp", "tcp", true, processes)...)
	ports = append(ports, parseLinuxNetFile("/proc/net/tcp6", "tcp6", true, processes)...)
	ports = append(ports, parseLinuxNetFile("/proc/net/udp", "udp", false, processes)...)
	ports = append(ports, parseLinuxNetFile("/proc/net/udp6", "udp6", false, processes)...)
	sortListeningPorts(ports)
	return ports
}

func parseLinuxNetFile(path, protocol string, tcp bool, processes map[string]listeningPort) []listeningPort {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var ports []listeningPort
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[0] == "sl" {
			continue
		}
		if tcp && fields[3] != "0A" {
			continue
		}

		address, port := parseLinuxSocketAddress(fields[1])
		inode := fields[9]
		process := processes[inode]
		ports = append(ports, listeningPort{
			Protocol: protocol,
			Address:  address,
			Port:     port,
			Process:  process.Process,
			PID:      process.PID,
		})
	}
	return ports
}

func linuxSocketProcessMap() map[string]listeningPort {
	result := map[string]listeningPort{}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return result
	}

	for _, proc := range procs {
		if !proc.IsDir() || !isDigits(proc.Name()) {
			continue
		}
		fdDir := filepath.Join("/proc", proc.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		name := firstNonEmpty(readFile(filepath.Join("/proc", proc.Name(), "comm")), readLinuxCmdline(proc.Name()), "N/A")
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || !strings.HasPrefix(target, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
			result[inode] = listeningPort{Process: name, PID: proc.Name()}
		}
	}
	return result
}

func readLinuxCmdline(pid string) string {
	data, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", " "))
}

func parseLinuxSocketAddress(value string) (string, string) {
	addressHex, portHex, ok := strings.Cut(value, ":")
	if !ok {
		return "N/A", "N/A"
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "N/A", "N/A"
	}
	if len(addressHex) == 8 {
		ip, err := strconv.ParseUint(addressHex, 16, 32)
		if err == nil {
			return net.IPv4(byte(ip), byte(ip>>8), byte(ip>>16), byte(ip>>24)).String(), strconv.FormatUint(port, 10)
		}
	}
	if len(addressHex) == 32 {
		decoded, err := hex.DecodeString(addressHex)
		if err == nil && len(decoded) == 16 {
			for i := 0; i < 16; i += 4 {
				decoded[i], decoded[i+3] = decoded[i+3], decoded[i]
				decoded[i+1], decoded[i+2] = decoded[i+2], decoded[i+1]
			}
			return net.IP(decoded).String(), strconv.FormatUint(port, 10)
		}
	}
	return "N/A", strconv.FormatUint(port, 10)
}

func windowsListeningPorts() []listeningPort {
	out, err := powershell(`
Get-NetTCPConnection -State Listen | ForEach-Object {
  $p = Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue
  "tcp|$($_.LocalAddress)|$($_.LocalPort)|$($_.OwningProcess)|$($p.ProcessName)"
}
Get-NetUDPEndpoint | ForEach-Object {
  $p = Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue
  "udp|$($_.LocalAddress)|$($_.LocalPort)|$($_.OwningProcess)|$($p.ProcessName)"
}
`)
	if err != nil {
		return nil
	}
	return parseDelimitedPorts(out)
}

func darwinListeningPorts() []listeningPort {
	if commandExists("lsof") {
		out, err := commandOutput(10*time.Second, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-iUDP")
		if err == nil {
			return parseLsofPorts(out)
		}
	}
	return nil
}

func parseDelimitedPorts(output string) []listeningPort {
	var ports []listeningPort
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) < 5 {
			continue
		}
		ports = append(ports, listeningPort{
			Protocol: parts[0],
			Address:  parts[1],
			Port:     parts[2],
			PID:      parts[3],
			Process:  parts[4],
		})
	}
	sortListeningPorts(ports)
	return ports
}

func parseLsofPorts(output string) []listeningPort {
	var ports []listeningPort
	for i, line := range strings.Split(output, "\n") {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		name := fields[0]
		pid := fields[1]
		protocol := strings.ToLower(fields[7])
		addressPort := fields[8]
		address, port := splitAddressPort(addressPort)
		ports = append(ports, listeningPort{Protocol: protocol, Address: address, Port: port, Process: name, PID: pid})
	}
	sortListeningPorts(ports)
	return ports
}

func splitAddressPort(value string) (string, string) {
	value = strings.TrimSuffix(value, "->")
	lastColon := strings.LastIndex(value, ":")
	if lastColon < 0 {
		return value, "N/A"
	}
	return value[:lastColon], value[lastColon+1:]
}

func sortListeningPorts(ports []listeningPort) {
	sort.Slice(ports, func(i, j int) bool {
		left := strings.ToLower(ports[i].Protocol) + ":" + ports[i].Port + ":" + ports[i].Address
		right := strings.ToLower(ports[j].Protocol) + ":" + ports[j].Port + ":" + ports[j].Address
		return left < right
	})
}

func listeningPortLine(port listeningPort) string {
	process := firstNonEmpty(port.Process, "N/A")
	if port.PID != "" {
		process = fmt.Sprintf("%s pid=%s", process, port.PID)
	}
	return fmt.Sprintf("%s %s:%s (%s)", strings.ToUpper(port.Protocol), firstNonEmpty(port.Address, "N/A"), firstNonEmpty(port.Port, "N/A"), process)
}
