package config

import (
	"fmt"
	"sort"
	"testing"
	"time"
)

func vsMap(vsNames []string) map[string]*Vserver {
	vs := make(map[string]*Vserver)
	for _, n := range vsNames {
		vs[n] = &Vserver{}
	}
	return vs
}

func vsNameList(s, l int) []string {
	var ret []string
	for i := s; i < s+l; i++ {
		ret = append(ret, fmt.Sprintf("vs-%02d", i))
	}
	return ret
}

func TestRateLimit(t *testing.T) {
	tests := []struct {
		desc                   string
		oldVS                  []string
		moreInitConfig         bool
		expectedMoreInitConfig bool
		newVS                  []string
		wantLen                int
	}{
		{
			desc:    "no rate limit",
			oldVS:   vsNameList(1, 10),
			newVS:   vsNameList(1, 19),
			wantLen: 19,
		},
		{
			desc:    "create is skipped",
			oldVS:   vsNameList(1, 5),
			newVS:   append(vsNameList(1, 2), vsNameList(6, 8)...),
			wantLen: 2,
		},
		{
			desc:    "deletion is rate limited",
			oldVS:   vsNameList(1, 11),
			newVS:   nil,
			wantLen: 1,
		},
		{
			desc:    "creation is rate limited",
			oldVS:   vsNameList(1, 3),
			newVS:   vsNameList(1, 14),
			wantLen: 13,
		},
		{
			desc:    "old is nil",
			oldVS:   nil,
			newVS:   vsNameList(1, 14),
			wantLen: 10,
		},
		{
			desc:                   "moreInitConfig is true and rate limited",
			oldVS:                  vsNameList(1, 11),
			newVS:                  nil,
			moreInitConfig:         true,
			expectedMoreInitConfig: true,
			wantLen:                1,
		},
		{
			desc:                   "moreInitConfig is false and flipped",
			oldVS:                  vsNameList(1, 3),
			newVS:                  vsNameList(1, 4),
			moreInitConfig:         true,
			expectedMoreInitConfig: false,
			wantLen:                4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			oldVS := vsMap(tc.oldVS)
			newVS := vsMap(tc.newVS)
			newCluster := &Cluster{Vservers: newVS}
			newCluster.Status.LastUpdate = time.Now()
			gotMoreInitConfig := rateLimit(newCluster, &Cluster{Vservers: oldVS}, tc.moreInitConfig)

			if gotMoreInitConfig != tc.expectedMoreInitConfig {
				t.Fatalf("moreInitConfig is wrong (got vs want):%t, %t", gotMoreInitConfig, tc.expectedMoreInitConfig)
			}
			for _, vs := range newCluster.Vservers {
				if !gotMoreInitConfig && tc.moreInitConfig {
					if !vs.MustReady {
						t.Fatalf("vserver %s must have MustReady set", vs.Name)
					}
				} else {
					if vs.MustReady {
						t.Fatalf("vserver %s shouldn't have MustReady set", vs.Name)
					}
				}
			}

			var limited []string
			for n := range newCluster.Vservers {
				limited = append(limited, n)
			}
			if len(limited) != tc.wantLen {
				sort.Strings(limited)
				t.Fatalf("rate limited got: %v\nbut want %d items\n", limited, tc.wantLen)
			}
		})
	}

}
