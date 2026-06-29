package main

import (
	"math"
	"time"
)

const sampleInterval = time.Second

func collectSamples(interval time.Duration) sampleData {
	samples := sampleData{
		intervalSeconds: interval.Seconds(),
		cpuUtilization:  math.NaN(),
		diskRates:       map[string]diskRate{},
		networkRates:    map[string]networkRate{},
	}

	cpuBefore, cpuBeforeOK := readCPUTimes()
	diskBefore := readDiskCounters()
	netBefore := readNetworkCounters()

	time.Sleep(interval)

	cpuAfter, cpuAfterOK := readCPUTimes()
	diskAfter := readDiskCounters()
	netAfter := readNetworkCounters()

	if cpuBeforeOK && cpuAfterOK {
		samples.cpuUtilization = calculateCPUUtilization(cpuBefore, cpuAfter)
	} else if utilization, ok := commandCPUUtilization(); ok {
		samples.cpuUtilization = utilization
	}

	samples.diskRates = calculateDiskRates(diskBefore, diskAfter, samples.intervalSeconds)
	samples.networkRates = calculateNetworkRates(netBefore, netAfter, samples.intervalSeconds)

	return samples
}
