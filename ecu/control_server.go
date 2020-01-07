package ecu

import (
	"context"
	"fmt"
	"os"

	"github.com/golang/protobuf/ptypes"
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

// GetStats implements SeesawECU.GetStats
func (c *controlServer) GetStats(ctx context.Context, in *ecupb.GetStatsRequest) (*ecupb.SeesawStats, error) {
	out := &ecupb.SeesawStats{}
	hn, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("os.Hostname() failed: %v", err)
	}
	out.Hostname = hn
	ha, err := c.sc.GetHAStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get HA status: %v", err)
	}
	out.HaState = haStateToProto(ha.State)

	cs, err := c.sc.GetConfigStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get Config status: %v", err)
	}
	if !cs.LastUpdate.IsZero() {
		ts, err := ptypes.TimestampProto(cs.LastUpdate)
		if err != nil {
			return nil, fmt.Errorf("invalid timestamp: %v", err)
		}
		out.ConfigTimestamp = ts
	}
	return out, nil
}
