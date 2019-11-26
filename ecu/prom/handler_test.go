package prom

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

type fakeCollector struct {
	metric typedDesc
}

// Describe implements the prometheus.Collector interface.
func (f *fakeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- f.metric.desc
}

// Collect implements the prometheus.Collector interface.
func (f *fakeCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- f.metric.mustNewConstMetric(1.0)
}

func TestPromHandler(t *testing.T) {
	factories := map[string]func() (prometheus.Collector, error){
		"fake": func() (prometheus.Collector, error) {
			return &fakeCollector{
				metric: typedDesc{prometheus.NewDesc(
					prometheus.BuildFQName(namespace, "fake", "metric"),
					"Fake metric for testing.",
					nil, nil),
					prometheus.GaugeValue},
			}, nil
		},
	}
	handler, err := promHandler(factories)
	if err != nil {
		t.Fatalf("promHandler failed: %v", err)
	}

	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, &http.Request{})

	got, err := ioutil.ReadAll(rw.Body)
	if err != nil {
		t.Fatal(err)
	}

	if want := fmt.Sprintf("%s 1", prometheus.BuildFQName(namespace, "fake", "metric")); !strings.Contains(string(got), want) {
		t.Fatalf("response doesn't have expected metrics (got -> want):\n%s\n->\n%s", got, want)
	}
}
