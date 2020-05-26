package ecu

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/seesaw/common/conn"
	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"

	ecupb "github.com/google/seesaw/pb/ecu"
	spb "github.com/google/seesaw/pb/seesaw"
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

func haStateToProto(state spb.HaState) ecupb.SeesawStats_HAState {
	switch state {
	case spb.HaState_BACKUP:
		return ecupb.SeesawStats_HA_BACKUP
	case spb.HaState_DISABLED:
		return ecupb.SeesawStats_HA_DISABLED
	case spb.HaState_ERROR:
		return ecupb.SeesawStats_HA_ERROR
	case spb.HaState_LEADER:
		return ecupb.SeesawStats_HA_MASTER
	case spb.HaState_SHUTDOWN:
		return ecupb.SeesawStats_HA_SHUTDOWN
	default:
		return ecupb.SeesawStats_HA_UNKNOWN
	}
}

const (
	cfgReadinessThreshold = time.Minute * 2
	hcReadinessThreshold  = time.Minute * 2
)

func extractMoreInitConfigAttr(cs *seesaw.ConfigStatus) (bool, error) {
	for _, m := range cs.Attributes {
		if m.Name == seesaw.MoreInitConfigAttrName {
			b, err := strconv.ParseBool(m.Value)
			if err != nil {
				return false, fmt.Errorf("bad attribute format (%s): %v", m.Name, err)
			}
			return b, nil
		}
	}
	return false, fmt.Errorf("attribute %s not found", seesaw.MoreInitConfigAttrName)
}

/* checkReadiness verifies if the LB is ready or not. The criteria for readiness is:
*   The LB is either a BACKUP or MASTER.
*   It has received a fresh config push from vserver controller in the cluster. It's done by checking last config update timestamp.
*   more_init_config attribute is false in the current config. If it's true, it means the LB has never received a full config due to rate limitting so it needs to wait for one.
*   All vservers in the first full config must be ready. It's done by checking OldestHealthCheck is within a threshold.
*   The rest of ververs must have a fresh health check results or the health check is never done (OldestHealthCheck is zero).
*
* For example, if there are vs1-5 (vserver 1-5) in the first config.
Here is the timeline when LB is rebooted:
*   # LB runs ha component to have a role of MASTER or BACKUP.
*   # LB receives the first config contains vs1-4 (assuming rate limiting allows only four vservers)
*   # more_init_config attribute is set to true on the config and LB will not be ready because of that
*   # the 2nd config loaded contains all vs1-5
*   # more_init_config turns false in this config and all vservers vs1-5 is marked as MustReady
*   # LB is still not ready because vs1-5 don't have healthcheck results yet (because they "MustReady")
*   # Healthcheck results for all vs1-5 come in and LB turns ready because OldestHealthCheck is fresh
*   # Now there is a new vs6 comes in. more_init_config is still false because it won't change once becomes true
*   # vs6 is not "MustReady" so LB is still ready even though OldestHealthCheck of vs6 is zero
*/
func checkReadiness(stats *statsCache) (spb.HaState, string, error) {
	ha, err := stats.GetHAStatus()
	if err != nil {
		return spb.HaState_UNKNOWN, "", fmt.Errorf("failed to get HA status: %v", err)
	}
	state := ha.State
	if state != spb.HaState_BACKUP && state != spb.HaState_LEADER {
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
	more, err := extractMoreInitConfigAttr(cs)
	if err != nil {
		return state, "", err
	}
	if more {
		return state, "Processing init config.", nil
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
			// This vserver is in the init config. Must be ready.
			if vs.MustReady {
				return state, fmt.Sprintf("%q: Healthcheck is not done yet.", name), nil
			}
			continue
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
