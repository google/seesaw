package prom

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/procfs"

	log "github.com/golang/glog"
)

type memCollector struct {
	fs                procfs.FS
	memUsed, memTotal typedDesc
}

func newMemCollector() (prometheus.Collector, error) {
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		return nil, fmt.Errorf("procfs.NewDefaultFS failed: %v", err)
	}
	return newMemCollectorWithFS(fs)
}

func newMemCollectorWithFS(fs procfs.FS) (*memCollector, error) {
	if _, err := fs.Meminfo(); err != nil {
		return nil, fmt.Errorf("failed to parse meminfo: %v", err)
	}
	return &memCollector{
		fs: fs,
		memUsed: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_used_bytes"),
			"Memory being used.",
			nil, nil),
			prometheus.GaugeValue},
		memTotal: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "memory_total_bytes"),
			"Total memory of the instance.",
			nil, nil),
			prometheus.GaugeValue},
	}, nil
}

// Describe implements the prometheus.Collector interface.
func (m *memCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.memUsed.desc
	ch <- m.memTotal.desc
}

// Collect implements the prometheus.Collector interface.
func (m *memCollector) Collect(ch chan<- prometheus.Metric) {
	meminfo, err := m.fs.Meminfo()
	if err != nil {
		log.Fatalf("failed to parse meminfo: %v", err)
	}
	totalKiB := meminfo.MemTotal
	usedKiB := totalKiB - meminfo.MemAvailable
	ch <- m.memUsed.mustNewConstMetric(float64(usedKiB * 1024))
	ch <- m.memTotal.mustNewConstMetric(float64(totalKiB * 1024))
}
