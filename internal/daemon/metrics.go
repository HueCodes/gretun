//go:build linux

package daemon

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds the Prometheus collectors the daemon publishes. Only exposed
// if the user passes --metrics-addr; otherwise these stay registered against
// a discarded registry and cost close to nothing.
type Metrics struct {
	PeersByState      *prometheus.GaugeVec
	DiscoPingsSent    prometheus.Counter
	DiscoPongsRecv    prometheus.Counter
	HolePunchDuration prometheus.Histogram
}

// NewMetrics registers the collectors with reg and returns handles.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		PeersByState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "gretun",
			Name:      "peers",
			Help:      "Number of peers in each FSM state.",
		}, []string{"state"}),
		DiscoPingsSent: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "gretun",
			Name:      "disco_pings_sent_total",
			Help:      "Disco ping messages emitted on the disco socket.",
		}),
		DiscoPongsRecv: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "gretun",
			Name:      "disco_pongs_received_total",
			Help:      "Disco pong messages accepted from a peer.",
		}),
		HolePunchDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "gretun",
			Name:      "hole_punch_duration_seconds",
			Help:      "Elapsed wall time from entering `punching` to reaching `direct`.",
			Buckets:   []float64{0.25, 0.5, 1, 2, 5, 10, 30},
		}),
	}
	if reg != nil {
		reg.MustRegister(m.PeersByState, m.DiscoPingsSent, m.DiscoPongsRecv, m.HolePunchDuration)
	}
	return m
}
