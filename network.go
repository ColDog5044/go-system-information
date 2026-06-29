package main

import (
	"bufio"
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

func printNetwork(samples sampleData) {
	section("Network Adapters")

	interfaces, err := net.Interfaces()
	if err != nil {
		printKV(2, "Error", err.Error())
		return
	}

	sort.Slice(interfaces, func(i, j int) bool {
		return interfaces[i].Name < interfaces[j].Name
	})

	gateways := defaultGateways()
	dns := dnsServers()

	for _, iface := range interfaces {
		rate, rateOK := samples.networkRates[iface.Name]
		ipv4, ipv6, masks := interfaceAddresses(iface)

		group(iface.Name)
		printKV(4, "Throughput", formatOptionalBitsPerSecond(rate.bytesSentSec+rate.bytesRecvSec, rateOK))
		printKV(4, "Send", formatOptionalBitsPerSecond(rate.bytesSentSec, rateOK))
		printKV(4, "Receive", formatOptionalBitsPerSecond(rate.bytesRecvSec, rateOK))
		printKV(4, "MAC address", firstNonEmpty(iface.HardwareAddr.String(), "N/A"))
		printKV(4, "Connection type", connectionType(iface))
		printKV(4, "Link speed", linkSpeed(iface.Name))
		printKV(4, "MTU", strconv.Itoa(iface.MTU))
		printKV(4, "IPv4 address(es)", joinOrNA(ipv4))
		printKV(4, "IPv6 address(es)", joinOrNA(ipv6))
		printKV(4, "Subnet mask", joinOrNA(masks))
		printKV(4, "Default gateway", firstNonEmpty(gateways[iface.Name], gateways["*"], "N/A"))
		printKV(4, "DNS servers", joinOrNA(dns))
	}
}

func readNetworkCounters() map[string]networkCounter {
	switch runtime.GOOS {
	case "linux":
		return linuxNetworkCounters()
	case "windows":
		return windowsNetworkCounters()
	default:
		return map[string]networkCounter{}
	}
}

func linuxNetworkCounters() map[string]networkCounter {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return map[string]networkCounter{}
	}
	defer file.Close()

	counters := map[string]networkCounter{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") {
			continue
		}

		name, data, _ := strings.Cut(line, ":")
		fields := strings.Fields(data)
		if len(fields) < 16 {
			continue
		}

		name = strings.TrimSpace(name)
		counters[name] = networkCounter{
			name:      name,
			bytesRecv: parseUint(fields[0]),
			bytesSent: parseUint(fields[8]),
		}
	}

	return counters
}

func windowsNetworkCounters() map[string]networkCounter {
	out, err := powershell(`Get-NetAdapterStatistics | ForEach-Object { "$($_.Name):$($_.SentBytes):$($_.ReceivedBytes)" }`)
	if err != nil {
		return map[string]networkCounter{}
	}

	counters := map[string]networkCounter{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(line), ":")
		if len(parts) != 3 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		counters[name] = networkCounter{
			name:      name,
			bytesSent: parseUint(parts[1]),
			bytesRecv: parseUint(parts[2]),
		}
	}
	return counters
}

func calculateNetworkRates(before, after map[string]networkCounter, seconds float64) map[string]networkRate {
	rates := map[string]networkRate{}
	if seconds <= 0 {
		return rates
	}

	for name, end := range after {
		start, ok := before[name]
		if !ok {
			continue
		}

		rates[name] = networkRate{
			bytesSentSec: float64(deltaUint64(start.bytesSent, end.bytesSent)) / seconds,
			bytesRecvSec: float64(deltaUint64(start.bytesRecv, end.bytesRecv)) / seconds,
		}
	}

	return rates
}

func interfaceAddresses(iface net.Interface) (ipv4 []string, ipv6 []string, masks []string) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, nil, nil
	}

	for _, addr := range addrs {
		raw := strings.TrimSpace(addr.String())
		if raw == "" {
			continue
		}

		ip, network, err := net.ParseCIDR(raw)
		if err != nil {
			parsed := net.ParseIP(raw)
			if parsed == nil {
				continue
			}
			if parsed.To4() != nil {
				ipv4 = append(ipv4, parsed.String())
			} else {
				ipv6 = append(ipv6, parsed.String())
			}
			continue
		}

		if ip.To4() != nil {
			ipv4 = append(ipv4, ip.String())
			masks = append(masks, ipv4MaskString(network.Mask))
		} else {
			ones, _ := network.Mask.Size()
			ipv6 = append(ipv6, ip.String()+"/"+strconv.Itoa(ones))
		}
	}

	return uniqueStrings(ipv4), uniqueStrings(ipv6), uniqueStrings(masks)
}

func connectionType(iface net.Interface) string {
	name := strings.ToLower(iface.Name)

	switch {
	case iface.Flags&net.FlagLoopback != 0 || strings.HasPrefix(name, "lo"):
		return "Loopback"
	case strings.Contains(name, "wi-fi"), strings.Contains(name, "wifi"), strings.Contains(name, "wlan"), strings.HasPrefix(name, "wl"):
		return "Wireless"
	case strings.HasPrefix(name, "eth"), strings.HasPrefix(name, "en"), strings.HasPrefix(name, "em"):
		return "Ethernet"
	case strings.HasPrefix(name, "tun"), strings.HasPrefix(name, "tap"), strings.Contains(name, "vpn"):
		return "Tunnel/VPN"
	case strings.HasPrefix(name, "br"), strings.Contains(name, "bridge"):
		return "Bridge"
	}

	return "N/A"
}

func linkSpeed(name string) string {
	switch runtime.GOOS {
	case "linux":
		speed := readUintFile(filepath.Join("/sys/class/net", name, "speed"))
		if speed > 0 {
			return fmt.Sprintf("%d Mbps", speed)
		}
	case "windows":
		out, err := powershell(fmt.Sprintf(`$a = Get-NetAdapter -Name %s -ErrorAction SilentlyContinue; if ($a) { $a.LinkSpeed }`, psQuote(name)))
		if err == nil {
			return firstNonEmpty(strings.TrimSpace(out), "N/A")
		}
	}

	return "N/A"
}

func defaultGateways() map[string]string {
	switch runtime.GOOS {
	case "linux":
		return linuxDefaultGateways()
	case "darwin":
		return darwinDefaultGateways()
	case "windows":
		return windowsDefaultGateways()
	default:
		return map[string]string{}
	}
}

func linuxDefaultGateways() map[string]string {
	result := map[string]string{}
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return result
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		ip := parseLittleEndianHexIPv4(fields[2])
		if ip != "" {
			result[fields[0]] = ip
		}
	}
	return result
}

func darwinDefaultGateways() map[string]string {
	result := map[string]string{}
	out, err := commandOutput(3*time.Second, "route", "-n", "get", "default")
	if err != nil {
		return result
	}
	values := parseKeyValueLines(out)
	if iface, gateway := values["interface"], values["gateway"]; iface != "" && gateway != "" {
		result[iface] = gateway
	}
	return result
}

func windowsDefaultGateways() map[string]string {
	out, err := powershell(`Get-NetRoute -DestinationPrefix "0.0.0.0/0" | Sort-Object RouteMetric | Select-Object -First 1 | ForEach-Object { "$($_.InterfaceAlias): $($_.NextHop)" }`)
	if err != nil {
		return map[string]string{}
	}
	return parseKeyValueLines(out)
}

func dnsServers() []string {
	switch runtime.GOOS {
	case "linux":
		return linuxDNSServers()
	case "darwin":
		return darwinDNSServers()
	case "windows":
		return windowsDNSServers()
	default:
		return nil
	}
}

func linuxDNSServers() []string {
	file, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer file.Close()

	var servers []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "nameserver" {
			servers = append(servers, fields[1])
		}
	}
	return uniqueStrings(servers)
}

func darwinDNSServers() []string {
	out, err := commandOutput(3*time.Second, "scutil", "--dns")
	if err != nil {
		return nil
	}

	var servers []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver[") {
			_, value, ok := strings.Cut(line, ":")
			if ok {
				servers = append(servers, strings.TrimSpace(value))
			}
		}
	}
	return uniqueStrings(servers)
}

func windowsDNSServers() []string {
	out, err := powershell(`Get-DnsClientServerAddress | ForEach-Object { $_.ServerAddresses } | Where-Object { $_ } | Sort-Object -Unique`)
	if err != nil {
		return nil
	}

	var servers []string
	for _, line := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			servers = append(servers, trimmed)
		}
	}
	return uniqueStrings(servers)
}

func parseLittleEndianHexIPv4(value string) string {
	if len(value) != 8 {
		return ""
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return ""
	}

	return net.IPv4(byte(parsed), byte(parsed>>8), byte(parsed>>16), byte(parsed>>24)).String()
}

func ipv4MaskString(mask net.IPMask) string {
	if len(mask) == 4 {
		return net.IP(mask).String()
	}
	if len(mask) == 16 {
		return net.IP(mask[12:]).String()
	}
	return "N/A"
}
