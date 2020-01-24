package ecu

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/seesaw/common/conn"
	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"

	ecupb "github.com/google/seesaw/pb/ecu"
)

type controlServer struct {
	engineSocket string
	sc           *statsCache
}

func newControlServer(engineSocket string, sc *statsCache) *controlServer {
	return &controlServer{
		engineSocket: engineSocket,
		sc:           sc,
	}
}

// dialedSeesawIPC returns a dialed IPC connection to Seesaw engine.
func dialedSeesawIPC(socket string) (*conn.Seesaw, error) {
	ctx := ipc.NewTrustedContext(seesaw.SCECU)
	seesawConn, err := conn.NewSeesawIPC(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to engine: %v", err)
	}
	if err := seesawConn.Dial(socket); err != nil {
		return nil, fmt.Errorf("failed to connect to engine: %v", err)
	}
	return seesawConn, nil
}

// Failover implements SeesawECU.Failover
func (c *controlServer) Failover(ctx context.Context, in *ecupb.FailoverRequest) (*ecupb.FailoverResponse, error) {
	seesawConn, err := dialedSeesawIPC(c.engineSocket)
	if err != nil {
		return nil, err
	}
	defer seesawConn.Close()

	if err := seesawConn.Failover(); err != nil {
		return nil, fmt.Errorf("failover failed: %v", err)
	}
	return &ecupb.FailoverResponse{}, nil
}

func haStateToProto(state seesaw.HAState) ecupb.SeesawStats_HAState {
	switch state {
	case seesaw.HABackup:
		return ecupb.SeesawStats_HA_BACKUP
	case seesaw.HADisabled:
		return ecupb.SeesawStats_HA_DISABLED
	case seesaw.HAError:
		return ecupb.SeesawStats_HA_ERROR
	case seesaw.HAMaster:
		return ecupb.SeesawStats_HA_MASTER
	case seesaw.HAShutdown:
		return ecupb.SeesawStats_HA_SHUTDOWN
	default:
		return ecupb.SeesawStats_HA_UNKNOWN
	}
}

const (
	cfgReadinessThreshold = time.Minute * 2
	hcReadinessThreshold  = time.Minute * 2
)

func checkReadiness(stats *statsCache) (seesaw.HAState, string, error) {
	ha, err := stats.GetHAStatus()
	if err != nil {
		return seesaw.HAUnknown, "", fmt.Errorf("failed to get HA status: %v", err)
	}
	state := ha.State
	if state != seesaw.HABackup && state != seesaw.HAMaster {
		return state, fmt.Sprintf("HAState %q is neither master or backup.", state.String()), nil
	}

	cs, err := stats.GetConfigStatus()
	if err != nil {
		return state, "", fmt.Errorf("failed to get Config status: %v", err)
	}
	if cs.LastUpdate.IsZero() {
		return state, "No config pushed.", nil
	}
	now := time.Now()
	if now.Sub(cs.LastUpdate) >= cfgReadinessThreshold {
		return state, fmt.Sprintf("Last config push is too old (last vs now): %s vs %s", cs.LastUpdate.UTC().Format(time.UnixDate), now.UTC().Format(time.UnixDate)), nil
	}

	vservers, err := stats.GetVservers()
	if err != nil {
		return state, "", fmt.Errorf("failed to get Vservers: %v", err)
	}
	if len(vservers) == 0 {
		// If there is no service configured, consider it's ready.
		return state, "", nil
	}
	now = time.Now()
	for name, vs := range vservers {
		if vs.OldestHealthCheck.IsZero() {
			return state, fmt.Sprintf("%q: Healtheck is not done yet.", name), nil
		}
		if now.Sub(vs.OldestHealthCheck) >= hcReadinessThreshold {
			return state, fmt.Sprintf("%q: Oldest healthcheck is too old (last vs now): %s vs %s", name, vs.OldestHealthCheck.UTC().Format(time.UnixDate), now.UTC().Format(time.UnixDate)), nil
		}
	}

	return state, "", nil
}

// GetStats implements SeesawECU.GetStats
func (c *controlServer) GetStats(ctx context.Context, in *ecupb.GetStatsRequest) (*ecupb.SeesawStats, error) {
	out := &ecupb.SeesawStats{}
	hn, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("os.Hostname() failed: %v", err)
	}
	out.Hostname = hn

	ha, reason, err := checkReadiness(c.sc)
	if err != nil {
		return nil, err
	}

	out.HaState = haStateToProto(ha)
	out.Readiness = &ecupb.Readiness{
		IsReady: len(reason) == 0,
		Reason:  reason,
	}

	return out, nil
}
