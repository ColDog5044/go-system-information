package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

func printContainerVirtualizationInventory() {
	section("Containers & Virtualization")

	printKV(2, "Docker", dockerInventory())
	printKV(2, "Podman", podmanInventory())
	if runtime.GOOS == "windows" {
		printKV(2, "Hyper-V", hyperVInventory())
	}
	printKV(2, "Kubernetes", kubernetesInventory())
}

func dockerInventory() string {
	if !commandExists("docker") {
		return "Not detected"
	}

	version := commandValue(3*time.Second, "docker", "version", "--format", "{{.Server.Version}}")
	if version == "" {
		client := commandValue(3*time.Second, "docker", "version", "--format", "{{.Client.Version}}")
		if client == "" {
			return "Installed, daemon unavailable"
		}
		return fmt.Sprintf("Client %s, daemon unavailable", client)
	}

	containers := shellCount("docker ps -aq 2>/dev/null")
	running := shellCount("docker ps -q 2>/dev/null")
	images := shellCount("docker images -q 2>/dev/null | sort -u")
	volumes := shellCount("docker volume ls -q 2>/dev/null")
	networks := shellCount("docker network ls -q 2>/dev/null")

	return fmt.Sprintf("Server %s, containers=%s, running=%s, images=%s, volumes=%s, networks=%s",
		version, containers, running, images, volumes, networks)
}

func podmanInventory() string {
	if !commandExists("podman") {
		return "Not detected"
	}

	version := commandValue(3*time.Second, "podman", "version", "--format", "{{.Client.Version}}")
	if version == "" {
		version = commandValue(3*time.Second, "podman", "--version")
	}

	containers := shellCount("podman ps -aq 2>/dev/null")
	running := shellCount("podman ps -q 2>/dev/null")
	images := shellCount("podman images -q 2>/dev/null | sort -u")
	volumes := shellCount("podman volume ls -q 2>/dev/null")
	networks := shellCount("podman network ls -q 2>/dev/null")

	return fmt.Sprintf("%s, containers=%s, running=%s, images=%s, volumes=%s, networks=%s",
		firstNonEmpty(version, "Installed"), containers, running, images, volumes, networks)
}

func hyperVInventory() string {
	out, err := powershell(`
$feature = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V-All -ErrorAction SilentlyContinue
$service = Get-Service vmms -ErrorAction SilentlyContinue
$vms = @()
if (Get-Command Get-VM -ErrorAction SilentlyContinue) {
  $vms = @(Get-VM -ErrorAction SilentlyContinue)
}
$enabled = if ($feature) { $feature.State } else { "N/A" }
$serviceState = if ($service) { $service.Status } else { "Not detected" }
"Feature=$enabled, Service=$serviceState, VMs=$($vms.Count)"
`)
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(out), "N/A")
}

func kubernetesInventory() string {
	var parts []string

	if commandExists("kubectl") {
		version := commandValue(5*time.Second, "kubectl", "version", "--client=true")
		context := commandValue(3*time.Second, "kubectl", "config", "current-context")
		parts = append(parts, "kubectl "+firstNonEmpty(compactWhitespace(version), "installed"))
		if context != "" {
			parts = append(parts, "context="+context)
		}
	}

	switch runtime.GOOS {
	case "linux":
		for _, svc := range []string{"kubelet.service", "k3s.service", "k3s-agent.service", "microk8s.daemon-kubelet.service"} {
			if systemctlUnitExists(svc) {
				parts = append(parts, svc+"="+firstNonEmpty(systemctlUnitState(svc), "detected"))
			}
		}
		for _, tool := range []string{"minikube", "kind", "k3d", "microk8s"} {
			if commandExists(tool) {
				parts = append(parts, tool+" installed")
			}
		}
	case "windows", "darwin":
		for _, tool := range []string{"minikube", "kind", "k3d"} {
			if commandExists(tool) {
				parts = append(parts, tool+" installed")
			}
		}
	}

	if len(parts) == 0 {
		return "Not detected"
	}
	return strings.Join(uniqueStrings(parts), "; ")
}

func commandValue(timeout time.Duration, name string, args ...string) string {
	out, err := commandOutput(timeout, name, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func shellCount(command string) string {
	var out string
	var err error
	if runtime.GOOS == "windows" {
		out, err = powershell(fmt.Sprintf(`$items = @(%s); $items.Count`, command))
	} else {
		out, err = commandOutput(5*time.Second, "sh", "-c", command+" | wc -l")
	}
	if err != nil {
		return "N/A"
	}
	return firstNonEmpty(strings.TrimSpace(out), "0")
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
