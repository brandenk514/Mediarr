// Metrics endpoint (M7 #46): an auth-gated Prometheus scrape at GET
// /metrics. It reuses the same bearer-token auth as the rest of the API, so a
// Prometheus scrape config needs the service's token (single-user v1).
package api

import (
	"net/http"

	"github.com/brandenk514/mediarr/internal/metrics"
)

// MetricsDeps bundles the observability wiring for GET /metrics.
type MetricsDeps struct {
	// M is the shared metrics registry holder. A nil M leaves the endpoint
	// unregistered (health-only boot stays possible, as with other deps).
	M *metrics.Metrics
}

// SetMetrics wires the auth-gated /metrics endpoint. It also feeds the HTTP
// request metrics into the middleware chain; call it before Handler() is
// first used.
func (s *Server) SetMetrics(deps *MetricsDeps) {
	s.metricsDeps = deps
	s.metrics = deps.M
	if deps.M != nil {
		s.register("GET /metrics", s.requireAuth(s.Metrics))
	}
}

// Metrics serves a Prometheus scrape.
func (s *Server) Metrics(w http.ResponseWriter, r *http.Request) {
	s.metricsDeps.M.Handler().ServeHTTP(w, r)
}
