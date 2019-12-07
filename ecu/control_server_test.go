package ecu

import (
	"context"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/seesaw/common/seesaw"

	ecupb "github.com/google/seesaw/pb/ecu"
)

func TestGetStats(t *testing.T) {
	cache := newStatsCache("", time.Hour)
	cache.lastRefresh = time.Now()

	cache.ha = &seesaw.HAStatus{
		State: seesaw.HABackup,
	}
	lastUpdate := time.Date(2000, 2, 1, 12, 30, 0, 0, time.UTC)
	cache.cs = &seesaw.ConfigStatus{
		LastUpdate: lastUpdate,
	}

	server := newControlServer("", cache)
	got, err := server.GetStats(context.Background(), &ecupb.GetStatsRequest{})
	if len(got.Hostname) == 0 {
		t.Fatalf("hostname is empty")
	}
	got.Hostname = ""

	ts, err := ptypes.TimestampProto(lastUpdate)
	if err != nil {
		t.Fatal(err)
	}
	want := &ecupb.SeesawStats{
		HaState:         ecupb.SeesawStats_HA_BACKUP,
		ConfigTimestamp: ts,
	}

	if !proto.Equal(got, want) {
		t.Fatalf("got:\n%s\nwant\n:%s\n", got, want)
	}

	// check zero time tranlates to nil ConfigTimestamp
	cache.cs.LastUpdate = time.Time{}
	got2, err := server.GetStats(context.Background(), &ecupb.GetStatsRequest{})
	if got2.ConfigTimestamp != nil {
		t.Fatalf("Zero time isn't returned as nil: %s", got2.ConfigTimestamp)
	}
}
