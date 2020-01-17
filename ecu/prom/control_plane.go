package prom

import (
	"github.com/google/seesaw/common/seesaw"
	"github.com/prometheus/client_golang/prometheus"

	log "github.com/golang/glog"
)

type cpCollector struct {
	sp statsProvider

	isMaster typedDesc
	uptime   typedDesc
}

func newCPCollector(sp statsProvider) (prometheus.Collector, error) {
	return &cpCollector{
		sp: sp,
		isMaster: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "is_master"),
			"A bool value showing if current instance is master.",
			nil, nil),
			prometheus.GaugeValue},
		uptime: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "uptime"),
			"Duration in second that Seesaw engine has been running.",
			nil, nil),
			prometheus.CounterValue},
	}, nil
}

// Describe implements the prometheus.Collector interface.
func (c *cpCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.isMaster.desc
	ch <- c.uptime.desc
}

// Collect implements the prometheus.Collector interface.
func (c *cpCollector) Collect(ch chan<- prometheus.Metric) {
	ha, err := c.sp.GetHAStatus()
	if err != nil {
		log.Errorf("failed to get LB HAStatus: %v", err)
		return
	}
	var isMaster float64
	if ha.State == seesaw.HAMaster {
		isMaster = 1
	}
	ch <- c.isMaster.mustNewConstMetric(isMaster)

	es, err := c.sp.GetEngineStatus()
	if err != nil {
		log.Errorf("failed to get EngineStatus: %v", err)
		return
	}
	ch <- c.uptime.mustNewConstMetric(float64(es.Uptime.Seconds()))
}
