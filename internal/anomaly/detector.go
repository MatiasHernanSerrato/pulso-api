// Package anomaly encapsula la deteccion de outliers en series de peso.
// La idea es simple: guardamos todas las mediciones en weights y cada tanto
// corremos un z-score contra una ventana movil para marcar lecturas raras.
//
// Por que z-score y no algo mas sofisticado:
//   - Pulso maneja pocos puntos por usuario por dia. Algoritmos como DBSCAN
//     o isolation forest son overkill y requieren tuning por usuario.
//   - Con una ventana corta (28 dias) y un umbral de 2.5 sigmas agarramos
//     bastante bien las lecturas que la gente quiere revisar: balanza mal
//     calibrada, pata de la mesa floja, mediciones con ropa puesta.
//   - Es trivial de explicar en el dashboard, que es medio el punto.
//
// Limitaciones: no detecta tendencias, solo outliers puntuales. Para drift
// vamos a necesitar otra cosa (CUSUM o similar), pero eso queda para despues.
package anomaly

import (
	"errors"
	"math"
	"sort"
	"time"
)

// Severity clasifica cuan raro es el outlier. Lo usa el frontend para pintar
// el chip con color distinto y la app mobile para decidir si vibra.
type Severity string

const (
	SeverityNone   Severity = "none"
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// Reading es un punto de la serie, independiente del modelo de dominio.
// El caller mapea desde weights.Weight (o de donde sea) a esto.
type Reading struct {
	At    time.Time
	Value float64
}

// Anomaly es el resultado: la lectura que se marco como fuera de rango,
// mas el contexto que usamos para decidirlo. El frontend muestra todo.
type Anomaly struct {
	At         time.Time
	Value      float64
	Mean       float64
	StdDev     float64
	ZScore     float64
	WindowDays int
	WindowSize int
	Severity   Severity
}

// Config define la ventana y los umbrales. Los defaults estan tuneados
// para mediciones diarias o quasi-diarias; si alguien empieza a pesarse
// varias veces por dia conviene subir MinSamples.
type Config struct {
	// WindowDays es el largo de la ventana movil que miramos para armar
	// el baseline. Mas chico = reacciona mas rapido pero mete ruido.
	WindowDays int

	// MinSamples minima cantidad de puntos en la ventana para que tenga
	// sentido calcular z-score. Con menos de esto devolvemos SeverityNone.
	MinSamples int

	// LowZ, MediumZ, HighZ son los cortes de |z| para cada severidad.
	// |z| < LowZ => none. [LowZ, MediumZ) => low. Etc.
	LowZ    float64
	MediumZ float64
	HighZ   float64
}

// DefaultConfig devuelve los valores que usamos en prod por defecto.
// Tuneado a ojo despues de probar con mis propias mediciones de un año.
func DefaultConfig() Config {
	return Config{
		WindowDays: 28,
		MinSamples: 5,
		LowZ:       2.0,
		MediumZ:    2.5,
		HighZ:      3.5,
	}
}

// ErrInvalidConfig vuela cuando la config no tiene sentido (umbrales mal
// ordenados, ventana negativa). Fallamos en la construccion del Detector,
// no en cada Detect, asi los bugs se ven antes.
var ErrInvalidConfig = errors.New("anomaly: config invalida")

// Detector corre el z-score. No tiene estado compartido entre llamadas,
// es safe de usar de varias goroutines.
type Detector struct {
	cfg Config
}

// New arma un detector y valida la config. Si no te gusta la validacion
// y queres algo mas permisivo, usa NewUnchecked.
func New(cfg Config) (*Detector, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return &Detector{cfg: cfg}, nil
}

// NewUnchecked arma un detector sin validar. Util para tests que quieren
// pasar config degenerada a proposito.
func NewUnchecked(cfg Config) *Detector {
	return &Detector{cfg: cfg}
}

// Config devuelve la config actual. Util para el endpoint que muestra
// con que parametros se corrio la deteccion.
func (d *Detector) Config() Config { return d.cfg }

// Detect toma una serie ordenada de lecturas (o no, la ordenamos nosotros)
// y devuelve las que son outliers segun la ventana movil anterior a cada una.
// Nunca mira el futuro: para la lectura en t solo se usan las lecturas en
// (t - WindowDays, t). Asi la salida es estable y no depende de si despues
// se cargo algo mas.
func (d *Detector) Detect(readings []Reading) []Anomaly {
	if len(readings) == 0 {
		return nil
	}

	// Copia defensiva: no queremos reordenar el slice del caller.
	rs := make([]Reading, len(readings))
	copy(rs, readings)
	sort.Slice(rs, func(i, j int) bool { return rs[i].At.Before(rs[j].At) })

	var out []Anomaly
	window := time.Duration(d.cfg.WindowDays) * 24 * time.Hour

	// Indice del primer elemento dentro de la ventana. Lo movemos para
	// adelante a medida que avanzamos, asi cada lectura es O(1) amortizado.
	left := 0
	for i, r := range rs {
		cutoff := r.At.Add(-window)
		for left < i && rs[left].At.Before(cutoff) {
			left++
		}

		// La ventana es [left, i). Si no hay suficientes, skip.
		n := i - left
		if n < d.cfg.MinSamples {
			continue
		}

		mean, stddev := meanStdDev(rs[left:i])
		if stddev == 0 {
			// Todos iguales y r.Value distinto es tecnicamente anomalia
			// infinita, pero la tratamos como "no evaluable" para no mentir
			// con un z= +Inf en el UI.
			continue
		}

		z := (r.Value - mean) / stddev
		sev := d.severity(z)
		if sev == SeverityNone {
			continue
		}

		out = append(out, Anomaly{
			At:         r.At,
			Value:      r.Value,
			Mean:       mean,
			StdDev:     stddev,
			ZScore:     z,
			WindowDays: d.cfg.WindowDays,
			WindowSize: n,
			Severity:   sev,
		})
	}

	return out
}

// severity mapea |z| a Severity segun los cortes de la config.
func (d *Detector) severity(z float64) Severity {
	abs := math.Abs(z)
	switch {
	case abs >= d.cfg.HighZ:
		return SeverityHigh
	case abs >= d.cfg.MediumZ:
		return SeverityMedium
	case abs >= d.cfg.LowZ:
		return SeverityLow
	default:
		return SeverityNone
	}
}

// meanStdDev calcula media y desvio muestral (n-1) de la ventana.
// Para n==1 stddev es 0: ningun outlier se puede marcar contra un solo
// punto y Detect ya corta antes por MinSamples, pero defendemos igual.
func meanStdDev(rs []Reading) (float64, float64) {
	if len(rs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, r := range rs {
		sum += r.Value
	}
	mean := sum / float64(len(rs))

	if len(rs) < 2 {
		return mean, 0
	}

	var sqSum float64
	for _, r := range rs {
		d := r.Value - mean
		sqSum += d * d
	}
	variance := sqSum / float64(len(rs)-1)
	return mean, math.Sqrt(variance)
}

func validateConfig(c Config) error {
	if c.WindowDays <= 0 {
		return errors.New("anomaly: WindowDays debe ser > 0")
	}
	if c.MinSamples < 2 {
		return errors.New("anomaly: MinSamples debe ser >= 2")
	}
	if c.LowZ <= 0 || c.MediumZ <= c.LowZ || c.HighZ <= c.MediumZ {
		return errors.New("anomaly: cortes de z-score deben ser 0 < Low < Medium < High")
	}
	return nil
}
