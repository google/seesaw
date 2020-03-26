package ecu

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/seesaw/common/seesaw"

	ecupb "github.com/google/seesaw/pb/ecu"
)

func vsOf(oldestHCTime time.Time, mustReady bool) *seesaw.Vserver {
	return &seesaw.Vserver{
		OldestHealthCheck: oldestHCTime,
		MustReady:         mustReady,
	}
}

func TestGetStats(t *testing.T) {
	tests := []struct {
		desc           string
		ha             seesaw.HAState
		configTS       time.Time
		moreInitConfig bool
		vsList         map[string]*seesaw.Vserver
		reasonContains string
	}{
		{
			desc:     "ready as master",
			ha:       seesaw.HAMaster,
			configTS: time.Now(),
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Now(), false),
			},
		},
		{
			desc:     "ready as backup",
			ha:       seesaw.HABackup,
			configTS: time.Now(),
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Now(), false),
			},
		},
		{
			desc:     "ready multiple svc",
			ha:       seesaw.HAMaster,
			configTS: time.Now(),
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Now(), false),
				"svc-2": vsOf(time.Now(), false),
			},
		},
		{
			desc:     "not ready - HAState",
			ha:       seesaw.HAShutdown,
			configTS: time.Now(),
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Now(), false),
			},
			reasonContains: "neither master or backup",
		},
		{
			desc:     "not ready - no config push",
			ha:       seesaw.HAMaster,
			configTS: time.Time{},
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Now(), false),
			},
			reasonContains: "No config pushed",
		},
		{
			desc:     "not ready - config too old",
			ha:       seesaw.HAMaster,
			configTS: time.Now().Add(-cfgReadinessThreshold),
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Now(), false),
			},
			reasonContains: "Last config push is too old",
		},
		{
			desc:           "not ready - more init config",
			ha:             seesaw.HABackup,
			configTS:       time.Now(),
			moreInitConfig: true,
			reasonContains: "Processing init config",
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
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Time{}, true),
				"svc-2": vsOf(time.Now(), false),
			},
			reasonContains: "Healtheck is not done yet",
		},
		{
			desc:     "ready - healthcheck is not done but vs is not required",
			ha:       seesaw.HABackup,
			configTS: time.Now(),
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Time{}, false),
			},
		},
		{
			desc:     "not ready - healthcheck too old",
			ha:       seesaw.HABackup,
			configTS: time.Now(),
			vsList: map[string]*seesaw.Vserver{
				"svc-1": vsOf(time.Now(), false),
				"svc-2": vsOf(time.Now().Add(-hcReadinessThreshold), false),
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
				Attributes: []seesaw.ConfigMetadata{
					{Name: seesaw.MoreInitConfigAttrName, Value: strconv.FormatBool(tc.moreInitConfig)},
				},
			}
			cache.vs = tc.vsList
			if cache.vs == nil {
				cache.vs = make(map[string]*seesaw.Vserver)
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
