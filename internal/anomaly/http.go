package anomaly

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// Loader es el adapter contra la fuente de datos. Lo tenemos como interface
// para que el handler no dependa del servicio de weights directamente;
// en tests se cablea con un stub, en prod con el adapter real que vive en
// cmd/server.
type Loader interface {
	LoadReadings(r *http.Request, from, to time.Time) ([]Reading, error)
}

// Handler arma el http.Handler que corre la deteccion y devuelve los
// outliers como JSON. El contrato:
//   GET /api/anomaly/weights?days=90
//   -> 200 con {"config":..., "anomalies":[...]}
//   -> 400 si days no parsea o es <=0
//   -> 500 si el loader explota
//
// El auth middleware va antes de esta ruta, no aca, para mantener el
// handler puro.
func Handler(det *Detector, loader Loader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if det == nil || loader == nil {
			http.Error(w, "anomaly handler mal configurado", http.StatusInternalServerError)
			return
		}
		days, err := parseDays(r.URL.Query().Get("days"), 90)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		now := time.Now().UTC()
		from := now.AddDate(0, 0, -days)

		readings, err := loader.LoadReadings(r, from, now)
		if err != nil {
			http.Error(w, "no pude cargar lecturas", http.StatusInternalServerError)
			return
		}

		anomalies := det.Detect(readings)

		resp := struct {
			Config    Config     `json:"config"`
			Days      int        `json:"days"`
			Anomalies []Anomaly  `json:"anomalies"`
			Generated time.Time  `json:"generated_at"`
		}{
			Config:    det.Config(),
			Days:      days,
			Anomalies: anomalies,
			Generated: now,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			// no hay mucho que hacer: ya escribimos status implicito. Loguear
			// se encarga el middleware de arriba.
			return
		}
	}
}

// ErrInvalidDays se devuelve cuando el query param no parsea o esta fuera
// de rango. Lo expongo como variable para que los tests puedan comparar
// con errors.Is si hace falta.
var ErrInvalidDays = errors.New("param days invalido")

func parseDays(raw string, def int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrInvalidDays
	}
	if n <= 0 || n > 365*2 {
		return 0, ErrInvalidDays
	}
	return n, nil
}
