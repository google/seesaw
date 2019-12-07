package config

import (
	"sync"
	"time"

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
	if p.config.Metadata == nil {
		p.config.Metadata = &pb.Metadata{}
	}
	p.config.Metadata.LastUpdated = proto.Int64(time.Now().Unix())
	p.config.Vserver = vs
}
