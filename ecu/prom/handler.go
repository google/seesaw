package prom

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	log "github.com/golang/glog"
)

type errLogger struct{}

func (l errLogger) Println(v ...interface{}) {
	log.Errorln(v...)
}

// NewHandler creates an http.Handler with a list of prometheus collectors registered.
func NewHandler() (http.Handler, error) {
	factories := map[string]func() (prometheus.Collector, error){
		"mem": newMemCollector,
	}
	return promHandler(factories)
}

func promHandler(factories map[string]func() (prometheus.Collector, error)) (http.Handler, error) {
	r := prometheus.NewRegistry()
	for n, f := range factories {
		c, err := f()
		if err != nil {
			return nil, fmt.Errorf("failed to create collector %q: %v", n, err)
		}
		if err := r.Register(c); err != nil {
			return nil, fmt.Errorf("couldn't register prometheus collector %q: %v", n, err)
		}
		log.Infof("Enabled prometheus collector %q", n)
	}

	return promhttp.HandlerFor(
		prometheus.Gatherers{r},
		promhttp.HandlerOpts{
			ErrorLog:            errLogger{},
			ErrorHandling:       promhttp.HTTPErrorOnError,
			MaxRequestsInFlight: 10,
			Registry:            nil,
		},
	), nil

}
