package prom

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/ecu/prom/testutil"
	spb "github.com/google/seesaw/pb/seesaw"
	"github.com/prometheus/client_golang/prometheus"
)

type fakeStats struct {
	ha *seesaw.HAStatus
	cs *seesaw.ConfigStatus
	vs map[string]*seesaw.Vserver
	es *seesaw.EngineStatus
}

func (f *fakeStats) GetHAStatus() (*seesaw.HAStatus, error) {
	return f.ha, nil
}
func (f *fakeStats) GetConfigStatus() (*seesaw.ConfigStatus, error) {
	return f.cs, nil
}
func (f *fakeStats) GetVservers() (map[string]*seesaw.Vserver, error) {
	return f.vs, nil
}
func (f *fakeStats) GetEngineStatus() (*seesaw.EngineStatus, error) {
	return f.es, nil
}

func TestCPCollector(t *testing.T) {
	f := &fakeStats{
		ha: &seesaw.HAStatus{
			State: spb.HaState_LEADER,
		},
		es: &seesaw.EngineStatus{
			Uptime: time.Second * 10,
		},
	}

	c, err := newCPCollector(f)
	if err != nil {
		t.Fatalf("newCPCollector failed: %v", err)
	}
	got, err := testutil.DoCollect(c)
	if err != nil {
		t.Fatalf("failed to collect: %v", err)
	}
	expected := []string{
		fmt.Sprintf("%s 1", prometheus.BuildFQName(namespace, "", "is_master")),
		fmt.Sprintf("%s 10", prometheus.BuildFQName(namespace, "", "uptime")),
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
