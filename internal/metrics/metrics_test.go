package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHandlerExposesRegisteredMetrics(t *testing.T) {
	// Toco los contadores asi aparecen en el output.
	DBQueries.WithLabelValues("select").Inc()
	AlertsEmitted.WithLabelValues("weight_spike").Inc()
	WebhookEvents.WithLabelValues("mercadopago", "ok").Inc()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	text := string(body)
	for _, want := range []string{
		"pulso_db_queries_total",
		"pulso_alerts_emitted_total",
		"pulso_webhooks_events_total",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no encontre la metrica %s en el output", want)
		}
	}
}

func TestMiddlewareCuentaRequestsYLatencia(t *testing.T) {
	// Chi para que RouteContext devuelva el pattern.
	r := chi.NewRouter()
	r.Use(Middleware(func(req *http.Request) string {
		if rc := chi.RouteContext(req.Context()); rc != nil {
			return rc.RoutePattern()
		}
		return "unknown"
	}))
	r.Get("/ping/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL + "/ping/42")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		_ = resp.Body.Close()
	}

	// Pido /metrics directo al handler y chequeo que el counter subio.
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	text := rec.Body.String()
	if !strings.Contains(text, `pulso_http_requests_total{method="GET",route="/ping/{id}",status="418"} 3`) {
		t.Errorf("no veo 3 requests contados al pattern /ping/{id}. body:\n%s", text)
	}
}
