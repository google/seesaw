package testutil

import (
	"bytes"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// DoCollect generates metrics output from a collector.
func DoCollect(c prometheus.Collector) (string, error) {
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		return "", fmt.Errorf("registering collector failed: %v", err)

	}
	metrics, err := reg.Gather()
	if err != nil {
		return "", fmt.Errorf("gathering metrics failed: %v", err)
	}
	var gotBuf bytes.Buffer
	enc := expfmt.NewEncoder(&gotBuf, expfmt.FmtText)
	for _, mf := range metrics {
		if err := enc.Encode(mf); err != nil {
			return "", fmt.Errorf("encoding gathered metrics failed: %v", err)
		}
	}

	return gotBuf.String(), nil
}
