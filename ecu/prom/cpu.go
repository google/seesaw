package prom

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/procfs"

	log "github.com/golang/glog"
)

type cpuCollector struct {
	fs           procfs.FS
	cpuUsageTime typedDesc
}

func newCPUCollector() (prometheus.Collector, error) {
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		return nil, fmt.Errorf("procfs.NewDefaultFS failed: %v", err)
	}
	return newCPUCollectorWithFS(fs)
}

func newCPUCollectorWithFS(fs procfs.FS) (*cpuCollector, error) {
	if _, err := fs.Stat(); err != nil {
		return nil, fmt.Errorf("failed to parse stat: %v", err)
	}
	return &cpuCollector{
		fs: fs,
		cpuUsageTime: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "cpu_usage_time"),
			"Time in second that CPU is being used.",
			nil, nil),
			prometheus.CounterValue},
	}, nil
}

// Describe implements the prometheus.Collector interface.
func (c *cpuCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpuUsageTime.desc
}

// Collect implements the prometheus.Collector interface.
func (c *cpuCollector) Collect(ch chan<- prometheus.Metric) {
	stat, err := c.fs.Stat()
	if err != nil {
		log.Fatalf("failed to parse /proc/stat: %v", err)
	}
	cpuAll := stat.CPUTotal
	numCPU := len(stat.CPU)
	// add all columns but idle
	var cpuUsed float64 = cpuAll.User + cpuAll.Nice + cpuAll.System + cpuAll.Iowait + cpuAll.IRQ + cpuAll.SoftIRQ + cpuAll.Steal + cpuAll.Guest + cpuAll.GuestNice

	// divide by numCPU to normalize
	ch <- c.cpuUsageTime.mustNewConstMetric(cpuUsed / float64(numCPU))
}
