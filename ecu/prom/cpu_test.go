package prom

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/seesaw/ecu/prom/testutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/procfs"
)

func TestCPUCollector(t *testing.T) {
	fs, err := procfs.NewFS("testdata/proc")
	if err != nil {
		t.Fatalf("procfs.NewFS(testdata/proc) failed: %v", err)
	}

	c, err := newCPUCollectorWithFS(fs)
	if err != nil {
		t.Fatalf("newCPUCollectorWithFS failed: %v", err)
	}
	got, err := testutil.DoCollect(c)
	if err != nil {
		t.Fatalf("failed to collect: %v", err)
	}
	expected := []string{
		fmt.Sprintf("%s 0.06375", prometheus.BuildFQName(namespace, "", "cpu_usage_time")),
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
