package config

import (
	"sync"

	"github.com/golang/protobuf/proto"
	pb "github.com/google/seesaw/pb/config"
)

// Provider holds configs.
type Provider struct {
	mux    sync.Mutex
	config *pb.Cluster
}

// NewProvider returns a Provider
func NewProvider(initConfig *pb.Cluster) *Provider {
	return &Provider{
		config: initConfig,
	}
}

func (p *Provider) get() ([]byte, error) {
	p.mux.Lock()
	defer p.mux.Unlock()
	return proto.Marshal(p.config)
}

func (p *Provider) updateVservers(vs []*pb.Vserver) {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.config.Vserver = vs
}
