package config

import (
	"fmt"
	"sort"
	"testing"
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
		desc    string
		oldVS   []string
		newVS   []string
		wantLen int
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
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			oldVS := vsMap(tc.oldVS)
			newVS := vsMap(tc.newVS)
			limited := rateLimitVS(newVS, oldVS)
			var got []string
			for n := range limited {
				got = append(got, n)
			}
			if len(got) != tc.wantLen {
				sort.Strings(got)
				t.Fatalf("rate limited got: %v\nbut want %d items\n", got, tc.wantLen)
			}
		})
	}

}
