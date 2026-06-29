package main

type sampleData struct {
	intervalSeconds float64
	cpuUtilization  float64
	diskRates       map[string]diskRate
	networkRates    map[string]networkRate
}

type cpuTimes struct {
	user      float64
	nice      float64
	system    float64
	idle      float64
	iowait    float64
	irq       float64
	softirq   float64
	steal     float64
	guest     float64
	guestNice float64
}

type diskCounter struct {
	name        string
	readOps     uint64
	writeOps    uint64
	readBytes   uint64
	writeBytes  uint64
	readTimeMS  uint64
	writeTimeMS uint64
	ioTimeMS    uint64
}

type diskRate struct {
	activePercent float64
	readBytesSec  float64
	writeBytesSec float64
	transferSec   float64
	responseMS    float64
}

type networkCounter struct {
	name      string
	bytesSent uint64
	bytesRecv uint64
}

type networkRate struct {
	bytesSentSec float64
	bytesRecvSec float64
}

type systemInventory struct {
	bios         string
	productKey   string
	manufacturer string
	model        string
	serialNumber string
}

type cpuInventory struct {
	model         string
	vendor        string
	clockSpeed    string
	maxSpeed      string
	processors    string
	physicalCores string
	logicalCores  string
	externalSpeed string
	l1Cache       string
	l2Cache       string
	l3Cache       string
	architecture  string
}

type memoryInfo struct {
	usagePercent     string
	inUse            string
	available        string
	committed        string
	cached           string
	pagedPool        string
	nonPagedPool     string
	hardwareReserved string
	slotsUsed        string
}

type partitionStat struct {
	Device     string
	Mountpoint string
	FSType     string
	Opts       []string
}

type volumeUsage struct {
	Total       uint64
	Used        uint64
	Free        uint64
	UsedPercent float64
	FSType      string
}

type serviceInfo struct {
	Name        string
	DisplayName string
	State       string
	StartType   string
	Detail      string
}

type listeningPort struct {
	Protocol string
	Address  string
	Port     string
	Process  string
	PID      string
}

type softwarePackage struct {
	Name      string
	Version   string
	Publisher string
}

type userAccount struct {
	Name       string
	UID        string
	Disabled   string
	Privileged bool
	Detail     string
}
