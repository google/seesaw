package prom

import (
	"strconv"

	"github.com/google/seesaw/ipvs"
	"github.com/prometheus/client_golang/prometheus"

	log "github.com/golang/glog"
)

type serviceCollector struct {
	sp statsProvider

	ingressBytesByService     typedDesc
	ingressBytesByBackend     typedDesc
	packetsByService          typedDesc
	packetsByBackend          typedDesc
	activeConnectionByService typedDesc
	activeConnectionByBackend typedDesc
	healthyBackend            typedDesc
}

var (
	serviceLabelNames = []string{
		"service_name",
		"service_port",
		"service_proto",
	}
	backendLabelNames = []string{
		"backend_node",
	}
)

func newServiceCollector(sp statsProvider) (prometheus.Collector, error) {
	return &serviceCollector{
		sp: sp,
		ingressBytesByService: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "ingress_bytes_count_by_service"),
			"Number of ingress bytes per service.",
			serviceLabelNames, nil),
			prometheus.CounterValue},
		ingressBytesByBackend: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "ingress_bytes_count_by_backend"),
			"Number of ingress bytes per backend.",
			backendLabelNames, nil),
			prometheus.CounterValue},
		packetsByService: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "packets_count_by_service"),
			"Number of packets per service.",
			serviceLabelNames, nil),
			prometheus.CounterValue},
		packetsByBackend: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "packets_count_by_backend"),
			"Number of packets per backend.",
			backendLabelNames, nil),
			prometheus.CounterValue},
		activeConnectionByService: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "active_connection_by_service"),
			"Number of active connections per service.",
			serviceLabelNames, nil),
			prometheus.GaugeValue},
		activeConnectionByBackend: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "active_connection_by_backend"),
			"Number of active connections per backend.",
			backendLabelNames, nil),
			prometheus.GaugeValue},
		healthyBackend: typedDesc{prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "healthy_backend"),
			"Number of healthy backends per service.",
			[]string{"service_name"}, nil),
			prometheus.GaugeValue},
	}, nil
}

// Describe implements the prometheus.Collector interface.
func (c *serviceCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.ingressBytesByService.desc
	ch <- c.ingressBytesByBackend.desc
	ch <- c.packetsByService.desc
	ch <- c.packetsByBackend.desc
	ch <- c.activeConnectionByService.desc
	ch <- c.activeConnectionByBackend.desc
	ch <- c.healthyBackend.desc
}

// Collect implements the prometheus.Collector interface.
func (c *serviceCollector) Collect(ch chan<- prometheus.Metric) {
	vservers, err := c.sp.GetVservers()
	if err != nil {
		log.Errorf("failed to get vservers: %v", err)
		return
	}
	backendBytesPerBackend := make(map[string]uint64)
	packetsPerBackend := make(map[string]uint64)
	activeConnectionPerBackend := make(map[string]uint64)
	for name, vs := range vservers {
		healthyBackendReported := false
		for svcKey, svc := range vs.Services {
			numHealthyBackend := 0
			labelValues := []string{
				name,
				strconv.Itoa(int(svcKey.Port)),
				svcKey.Proto.String(),
			}
			// defaults to zero
			stats := &ipvs.ServiceStats{}
			// stats is nil if backend is unhealthy
			if svc.Stats != nil && svc.Stats.ServiceStats != nil {
				stats = svc.Stats.ServiceStats
			}
			ch <- c.ingressBytesByService.mustNewConstMetric(float64(stats.BytesIn), labelValues...)
			ch <- c.packetsByService.mustNewConstMetric(float64(stats.PacketsIn), labelValues...)
			var activeConns uint32
			for dstName, dst := range svc.Destinations {
				if dst.Healthy {
					numHealthyBackend++
				}
				// defaults to zero
				stats := &ipvs.DestinationStats{}
				// stats is nil if backend is unhealthy
				if dst.Stats != nil && dst.Stats.DestinationStats != nil {
					stats = dst.Stats.DestinationStats
				}
				backendBytesPerBackend[dstName] += uint64(stats.BytesIn)
				packetsPerBackend[dstName] += uint64(stats.PacketsIn)
				activeConnectionPerBackend[dstName] += uint64(stats.ActiveConns)
				activeConns += stats.ActiveConns
			}
			ch <- c.activeConnectionByService.mustNewConstMetric(float64(activeConns), labelValues...)
			// One vserver (service) may have multiple ports & proto but they share backends.
			// Only report healthy backend once.
			if !healthyBackendReported {
				healthyBackendReported = true
				ch <- c.healthyBackend.mustNewConstMetric(float64(numHealthyBackend), name)
			}
		}
	}

	for backend, n := range backendBytesPerBackend {
		ch <- c.ingressBytesByBackend.mustNewConstMetric(float64(n), backend)
	}
	for backend, n := range packetsPerBackend {
		ch <- c.packetsByBackend.mustNewConstMetric(float64(n), backend)
	}
	for backend, n := range activeConnectionPerBackend {
		ch <- c.activeConnectionByBackend.mustNewConstMetric(float64(n), backend)
	}
}
