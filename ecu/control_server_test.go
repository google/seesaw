package ecu

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/seesaw/common/seesaw"

	ecupb "github.com/google/seesaw/pb/ecu"
)

func TestGetStats(t *testing.T) {
	tests := []struct {
		desc           string
		ha             seesaw.HAState
		configTS       time.Time
		oldestHCTime   map[string]time.Time
		reasonContains string
	}{
		{
			desc:     "ready as master",
			ha:       seesaw.HAMaster,
			configTS: time.Now(),
			oldestHCTime: map[string]time.Time{
				"svc-1": time.Now(),
			},
		},
		{
			desc:     "ready as backup",
			ha:       seesaw.HABackup,
			configTS: time.Now(),
			oldestHCTime: map[string]time.Time{
				"svc-1": time.Now(),
			},
		},
		{
			desc:     "ready multiple svc",
			ha:       seesaw.HAMaster,
			configTS: time.Now(),
			oldestHCTime: map[string]time.Time{
				"svc-1": time.Now(),
				"svc-2": time.Now(),
			},
		},
		{
			desc:     "not ready - HAState",
			ha:       seesaw.HAShutdown,
			configTS: time.Now(),
			oldestHCTime: map[string]time.Time{
				"svc-1": time.Now(),
			},
			reasonContains: "neither master or backup",
		},
		{
			desc:     "not ready - no config push",
			ha:       seesaw.HAMaster,
			configTS: time.Time{},
			oldestHCTime: map[string]time.Time{
				"svc-1": time.Now(),
			},
			reasonContains: "No config pushed",
		},
		{
			desc:     "not ready - config too old",
			ha:       seesaw.HAMaster,
			configTS: time.Now().Add(-cfgReadinessThreshold),
			oldestHCTime: map[string]time.Time{
				"svc-1": time.Now(),
			},
			reasonContains: "Last config push is too old",
		},
		{
			desc:     "ready - no service",
			ha:       seesaw.HABackup,
			configTS: time.Now(),
		},
		{
			desc:     "not ready - healthcheck is not done",
			ha:       seesaw.HABackup,
			configTS: time.Now(),
			oldestHCTime: map[string]time.Time{
				"svc-1": time.Time{},
				"svc-2": time.Now(),
			},
			reasonContains: "Healtheck is not done yet",
		},
		{
			desc:     "not ready - healthcheck too old",
			ha:       seesaw.HABackup,
			configTS: time.Now(),
			oldestHCTime: map[string]time.Time{
				"svc-1": time.Now(),
				"svc-2": time.Now().Add(-hcReadinessThreshold),
			},
			reasonContains: "Oldest healthcheck is too old",
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			cache := newStatsCache("", time.Hour)
			cache.lastRefresh = time.Now()

			cache.ha = &seesaw.HAStatus{
				State: tc.ha,
			}
			cache.cs = &seesaw.ConfigStatus{
				LastUpdate: tc.configTS,
			}
			cache.vs = make(map[string]*seesaw.Vserver)
			for name, ts := range tc.oldestHCTime {
				cache.vs[name] = &seesaw.Vserver{
					OldestHealthCheck: ts,
				}
			}

			server := newControlServer("", cache)
			resp, err := server.GetStats(context.Background(), &ecupb.GetStatsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Hostname) == 0 {
				t.Fatalf("hostname is empty")
			}
			if got, want := resp.HaState, haStateToProto(tc.ha); got != want {
				t.Fatalf("Unexpected HAState (got vs want): %s vs %s", got, want)
			}
			if resp.GetReadiness() == nil {
				t.Fatalf("nil readiness in response")
			}
			readiness := resp.GetReadiness()
			if tc.reasonContains == "" {
				if !readiness.GetIsReady() {
					t.Fatalf("is not ready: %s", readiness.Reason)
				}
				return
			}
			if readiness.GetIsReady() {
				t.Fatalf("is ready but want not ready: %s", tc.reasonContains)
			}
			if got, want := readiness.Reason, tc.reasonContains; !strings.Contains(got, want) {
				t.Fatalf("reason doesn't contain expected msg (got vs want): %q vs %q", got, want)
			}
		})
	}
}
