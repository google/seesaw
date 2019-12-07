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
	cs  *seesaw.ConfigStatus
	err error
}

func (f *fakeIPC) HAStatus(ctx *ipc.Context, status *seesaw.HAStatus) error {
	if f.err != nil {
		return f.err
	}
	*status = *f.ha
	return nil
}

func (f *fakeIPC) ConfigStatus(ctx *ipc.Context, status *seesaw.ConfigStatus) error {
	if f.err != nil {
		return f.err
	}
	*status = *f.cs
	return nil
}

const (
	socketPath = "/tmp/fakeipc.socket"
	threshold  = time.Minute
)

func checkGet(cache *statsCache, expectedHA *seesaw.HAStatus, expectedCS *seesaw.ConfigStatus) error {
	ha, err := cache.getHAStatus()
	if err != nil {
		return fmt.Errorf("getHA: expect no error but got %v", err)
	}
	if got, want := ha, expectedHA; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("getHA: got %#v but want: %#v", got, want)
	}
	cs, err := cache.getConfigStatus()
	if err != nil {
		return fmt.Errorf("getConfigStatus: expect no error but got %v", err)
	}
	if got, want := cs, expectedCS; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("getConfigStatus: got %#v but want: %#v", got, want)
	}
	return nil
}

func TestRefresh(t *testing.T) {
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

	ha := &seesaw.HAStatus{State: seesaw.HABackup}
	fakeIPC.ha = ha
	cs := &seesaw.ConfigStatus{LastUpdate: time.Date(2000, 2, 1, 12, 30, 0, 0, time.UTC)}
	fakeIPC.cs = cs
	if err := checkGet(cache, ha, cs); err != nil {
		t.Fatalf("checkGetHA failed: %v", err)
	}

	// verify getHA returns cached status
	fakeIPC.ha = nil
	fakeIPC.cs = nil
	if err := checkGet(cache, ha, cs); err != nil {
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
	if _, err := cache.getHAStatus(); err == nil {
		t.Fatalf("expect error but get nil")
	}
}
