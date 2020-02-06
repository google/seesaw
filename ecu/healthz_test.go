package ecu

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/seesaw/common/seesaw"
)

var (
	addr = ":12345"
	url  = fmt.Sprintf("http://127.0.0.1%s/healthz", addr)
)

func TestHealthz(t *testing.T) {
	cache := newStatsCache("xx", time.Hour)
	cache.lastRefresh = time.Now()
	s := newHealthzServer(addr, cache)
	go s.run()
	defer s.shutdown()

	time.Sleep(100 * time.Millisecond)

	tests := []struct {
		state       seesaw.HAState
		cacheFailed bool
		method      string
		expectCode  int
	}{
		{state: seesaw.HABackup, method: http.MethodGet, expectCode: http.StatusOK},
		{state: seesaw.HAMaster, method: http.MethodGet, expectCode: http.StatusOK},
		{state: seesaw.HAUnknown, method: http.MethodGet, expectCode: http.StatusServiceUnavailable},
		{state: seesaw.HABackup, method: http.MethodPost, expectCode: http.StatusMethodNotAllowed},
		{cacheFailed: true, method: http.MethodGet, expectCode: http.StatusInternalServerError},
	}

	client := &http.Client{}
	for _, tc := range tests {
		if tc.cacheFailed {
			// triggers a error
			cache.ha = nil
		} else {
			cache.ha = &seesaw.HAStatus{
				State: tc.state,
			}
		}
		req, err := http.NewRequest(tc.method, url, nil)
		if err != nil {
			t.Errorf("http.NewRequest failed: %v", err)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("client.Do failed: %v", err)
			continue
		}

		if got, want := resp.StatusCode, tc.expectCode; got != want {
			t.Fatalf("got %d but want %d", got, want)
		}
	}
}