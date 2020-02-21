package prom

import (
	"strings"
	"testing"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/ecu/prom/testutil"
	"github.com/google/seesaw/ipvs"
)

func TestServiceCollector(t *testing.T) {
	svc1 := &seesaw.Service{
		Stats: &seesaw.ServiceStats{
			ServiceStats: &ipvs.ServiceStats{
				Stats: ipvs.Stats{
					BytesIn:   3000,
					PacketsIn: 30,
				},
			},
		},
		Destinations: map[string]*seesaw.Destination{
			"node-1": &seesaw.Destination{
				Stats: &seesaw.DestinationStats{
					DestinationStats: &ipvs.DestinationStats{
						ActiveConns: 10,
						Stats: ipvs.Stats{
							BytesIn:   1000,
							PacketsIn: 10,
						},
					},
				},
			},
			"node-2": &seesaw.Destination{
				Healthy: true,
				Stats: &seesaw.DestinationStats{
					DestinationStats: &ipvs.DestinationStats{
						ActiveConns: 20,
						Stats: ipvs.Stats{
							BytesIn:   2000,
							PacketsIn: 20,
						},
					},
				},
			},
		},
	}
	svc2 := &seesaw.Service{
		Stats: &seesaw.ServiceStats{
			ServiceStats: &ipvs.ServiceStats{
				Stats: ipvs.Stats{
					BytesIn:   1001,
					PacketsIn: 11,
				},
			},
		},
		Destinations: map[string]*seesaw.Destination{
			"node-1": &seesaw.Destination{
				Healthy: true,
				Stats: &seesaw.DestinationStats{
					DestinationStats: &ipvs.DestinationStats{
						ActiveConns: 11,
						Stats: ipvs.Stats{
							BytesIn:   1001,
							PacketsIn: 11,
						},
					},
				},
			},
			"node-2": &seesaw.Destination{
				Stats: &seesaw.DestinationStats{},
			},
		},
	}
	f := &fakeStats{
		vs: map[string]*seesaw.Vserver{
			"service-1": &seesaw.Vserver{
				Services: map[seesaw.ServiceKey]*seesaw.Service{
					seesaw.ServiceKey{Proto: seesaw.IPProtoTCP, Port: 1}: svc1,
					seesaw.ServiceKey{Proto: seesaw.IPProtoTCP, Port: 2}: svc2,
				},
			},
		},
	}
	c, err := newServiceCollector(f)
	if err != nil {
		t.Fatalf("newServiceCollector failed: %v", err)
	}
	got, err := testutil.DoCollect(c)
	if err != nil {
		t.Fatalf("failed to collect: %v", err)
	}
	expected := []string{
		`seesaw_healthy_backend{service_name="service-1"} 1`,
		`seesaw_ingress_bytes_count_by_backend{backend_node="node-1"} 2001`,
		`seesaw_ingress_bytes_count_by_backend{backend_node="node-2"} 2000`,
		`seesaw_ingress_bytes_count_by_service{service_name="service-1",service_port="1",service_proto="TCP"} 3000`,
		`seesaw_ingress_bytes_count_by_service{service_name="service-1",service_port="2",service_proto="TCP"} 1001`,
		`seesaw_packets_count_by_backend{backend_node="node-1"} 21`,
		`seesaw_packets_count_by_backend{backend_node="node-2"} 20`,
		`seesaw_packets_count_by_service{service_name="service-1",service_port="1",service_proto="TCP"} 30`,
		`seesaw_packets_count_by_service{service_name="service-1",service_port="2",service_proto="TCP"} 11`,
		`seesaw_active_connection_by_backend{backend_node="node-1"} 21`,
		`seesaw_active_connection_by_backend{backend_node="node-2"} 20`,
		`seesaw_active_connection_by_service{service_name="service-1",service_port="1",service_proto="TCP"} 30`,
		`seesaw_active_connection_by_service{service_name="service-1",service_port="2",service_proto="TCP"} 11`,
	}
	for _, e := range expected {
		if !strings.Contains(got, e) {
			t.Fatalf(`
collector output does not match expectation; want:
%s
got:
%s`, e, got)
		}
	}
}
