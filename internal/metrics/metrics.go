// Package metrics expone los contadores y histogramas de pulso-api.
// Nada muy sofisticado: los HTTP handlers y el worker de alertas incrementan
// metricas desde aca, y /metrics las sirve en el formato de prometheus.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry es el registry que usa la app. Lo expongo por si algun test quiere
// chequear los valores sin pasar por el handler HTTP.
var Registry = prometheus.NewRegistry()

var (
	httpRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "pulso",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Cantidad de requests HTTP recibidos, por ruta/metodo/status.",
		},
		[]string{"route", "method", "status"},
	)

	httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "pulso",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "Duracion de los requests HTTP en segundos.",
			// Buckets pensados para una API: <10ms es cache/healthz, >1s es
			// algo pesado (graphql con joins grandes o webhook con validacion).
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"route", "method"},
	)

	// DBQueries lo incrementa el wrapper de pgx cuando agreguemos tracing
	// real. Por ahora lo dejo registrado asi el dashboard no se rompe.
	DBQueries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "pulso",
			Subsystem: "db",
			Name:      "queries_total",
			Help:      "Queries a la db, por tipo (select/insert/update/delete).",
		},
		[]string{"op"},
	)

	// AlertsEmitted lo usa internal/alerts cuando dispara una alerta.
	AlertsEmitted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "pulso",
			Subsystem: "alerts",
			Name:      "emitted_total",
			Help:      "Alertas emitidas por el detector de anomalias.",
		},
		[]string{"kind"},
	)

	// WebhookEvents lo incrementa cada handler de webhook.
	WebhookEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "pulso",
			Subsystem: "webhooks",
			Name:      "events_total",
			Help:      "Webhooks recibidos por proveedor y resultado (ok/invalid/error).",
		},
		[]string{"provider", "result"},
	)
)

func init() {
	Registry.MustRegister(
		httpRequests,
		httpDuration,
		DBQueries,
		AlertsEmitted,
		WebhookEvents,
	)
}

// Handler devuelve el handler HTTP de prometheus listo para montar en /metrics.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// statusRecorder guarda el status code para que el middleware pueda registrarlo.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Middleware mide requests HTTP. routeFn recibe el request y devuelve la label
// de ruta que queremos usar; separo asi del chi.RouteContext porque algunos
// handlers (webhooks) no pasan por chi.
func Middleware(routeFn func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			route := routeFn(r)
			if route == "" {
				route = "unknown"
			}
			httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
			httpDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
		})
	}
}
