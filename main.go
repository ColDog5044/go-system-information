package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	format := flag.String("format", "console", "output format: console or json")
	flag.Parse()

	switch strings.ToLower(*format) {
	case "console", "text", "terminal":
		setReportRenderer(newConsoleRenderer(os.Stdout))
	case "json":
		setReportRenderer(newJSONRenderer(os.Stdout))
	default:
		fmt.Fprintf(os.Stderr, "unsupported format %q; use console or json\n", *format)
		os.Exit(2)
	}

	startReport("System Information", sampleInterval)

	samples := collectSamples(sampleInterval)

	printSystem(samples)
	printProcessor(samples)
	printMemory()
	printDisks(samples)
	printNetwork(samples)
	printPatchManagement()
	printSecurityPosture()
	printServices()
	printNetworkExposure()
	printSoftwareInventory()
	printUserInventory()
	printHardwareHealth()
	printContainerVirtualizationInventory()
	printOperationalInventory()

	if err := finishReport(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
