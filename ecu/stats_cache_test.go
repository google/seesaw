package ecu

import (
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/common/server"
)

type fakeIPC struct {
	ha  *seesaw.HAStatus
	err error
}

func (f *fakeIPC) HAStatus(ctx *ipc.Context, status *seesaw.HAStatus) error {
	if f.err != nil {
		return f.err
	}
	*status = *f.ha
	return nil
}

const (
	socketPath = "/tmp/fakeipc"
	threshold  = time.Minute
)

func checkGetHA(cache *statsCache, expected *seesaw.HAStatus) error {
	resp, err := cache.getHA()
	if err != nil {
		return fmt.Errorf("expect no error but got %v", err)
	}
	if got, want := resp, expected; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("got %#v but want: %#v", got, want)
	}
	return nil
}

func TestGetHARefresh(t *testing.T) {
	fakeServer := rpc.NewServer()
	fakeIPC := &fakeIPC{}
	fakeServer.RegisterName("SeesawEngine", fakeIPC)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer os.Remove(socketPath)
	defer ln.Close()

	go server.RPCAccept(ln, fakeServer)

	cache := newStatsCache(socketPath, threshold)

	ha1 := &seesaw.HAStatus{State: seesaw.HABackup}
	fakeIPC.ha = ha1
	if err := checkGetHA(cache, ha1); err != nil {
		t.Fatalf("checkGetHA failed: %v", err)
	}

	// verify getHA returns cached status
	fakeIPC.ha = &seesaw.HAStatus{State: seesaw.HAMaster}
	if err := checkGetHA(cache, ha1); err != nil {
		t.Fatalf("getHA not returning cached HAStatus: %v", err)
	}
}

func TestGetHAError(t *testing.T) {
	fakeServer := rpc.NewServer()
	fakeIPC := &fakeIPC{err: errors.New("error")}
	fakeServer.RegisterName("SeesawEngine", fakeIPC)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer os.Remove(socketPath)
	defer ln.Close()

	go server.RPCAccept(ln, fakeServer)

	cache := newStatsCache(socketPath, threshold)
	if _, err := cache.getHA(); err == nil {
		t.Fatalf("expect error but get nil")
	}
}
