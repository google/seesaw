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
	spb "github.com/google/seesaw/pb/seesaw"
)

type fakeIPC struct {
	ha  *seesaw.HAStatus
	cs  *seesaw.ConfigStatus
	vs  map[string]*seesaw.Vserver
	es  *seesaw.EngineStatus
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

func (f *fakeIPC) Vservers(ctx *ipc.Context, vs *seesaw.VserverMap) error {
	if f.err != nil {
		return f.err
	}
	vs.Vservers = f.vs
	return nil
}

func (f *fakeIPC) EngineStatus(ctx *ipc.Context, es *seesaw.EngineStatus) error {
	if f.err != nil {
		return f.err
	}
	*es = *f.es
	return nil
}

const (
	socketPath = "/tmp/fakeipc.socket"
	threshold  = time.Minute
)

func checkGet(cache *statsCache, expectedHA *seesaw.HAStatus, expectedCS *seesaw.ConfigStatus, expectedVS map[string]*seesaw.Vserver, expectedES *seesaw.EngineStatus) error {
	ha, err := cache.GetHAStatus()
	if err != nil {
		return fmt.Errorf("GetHAStatus: expect no error but got %v", err)
	}
	if got, want := ha, expectedHA; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("GetHAStatus: got %#v but want: %#v", got, want)
	}
	cs, err := cache.GetConfigStatus()
	if err != nil {
		return fmt.Errorf("GetConfigStatus: expect no error but got %v", err)
	}
	if got, want := cs, expectedCS; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("GetConfigStatus: got %#v but want: %#v", got, want)
	}
	vs, err := cache.GetVservers()
	if err != nil {
		return fmt.Errorf("GetVservers: expect no error but got %v", err)
	}
	if got, want := vs, expectedVS; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("GetVservers: got %#v but want: %#v", got, want)
	}
	es, err := cache.GetEngineStatus()
	if err != nil {
		return fmt.Errorf("GetEngineStatus: expect no error but got %v", err)
	}
	if got, want := es, expectedES; !reflect.DeepEqual(got, want) {
		return fmt.Errorf("GetEngineStatus: got %#v but want: %#v", got, want)
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

	ha := &seesaw.HAStatus{State: spb.HaState_BACKUP}
	fakeIPC.ha = ha
	cs := &seesaw.ConfigStatus{LastUpdate: time.Date(2000, 2, 1, 12, 30, 0, 0, time.UTC)}
	fakeIPC.cs = cs
	vs := map[string]*seesaw.Vserver{
		"service": &seesaw.Vserver{},
	}
	fakeIPC.vs = vs
	es := &seesaw.EngineStatus{
		Uptime: time.Minute,
	}
	fakeIPC.es = es
	if err := checkGet(cache, ha, cs, vs, es); err != nil {
		t.Fatalf("checkGet failed: %v", err)
	}

	// verify getHA returns cached status
	fakeIPC.ha = nil
	fakeIPC.cs = nil
	if err := checkGet(cache, ha, cs, vs, es); err != nil {
		t.Fatalf("not returning cached status: %v", err)
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
	if _, err := cache.GetHAStatus(); err == nil {
		t.Fatalf("expect error but get nil")
	}
}
