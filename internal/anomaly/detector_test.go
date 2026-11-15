package anomaly

import (
	"math"
	"testing"
	"time"
)

// baseTime la fijamos para que las ventanas por dia sean reproducibles
// sin pelearnos con zonas horarias ni DST.
var baseTime = time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)

// mkSeries arma una serie con un punto por dia, empezando en baseTime.
// Le paso los valores y me devuelve []Reading listo para Detect.
func mkSeries(values ...float64) []Reading {
	out := make([]Reading, len(values))
	for i, v := range values {
		out[i] = Reading{
			At:    baseTime.AddDate(0, 0, i),
			Value: v,
		}
	}
	return out
}

func TestDetect_TableDriven(t *testing.T) {
	t.Parallel()

	cfg := Config{
		WindowDays: 28,
		MinSamples: 5,
		LowZ:       2.0,
		MediumZ:    2.5,
		HighZ:      3.5,
	}

	cases := []struct {
		name          string
		readings      []Reading
		wantCount     int
		wantIdx       []int // indices de readings que esperamos marcar
		wantSeverity  []Severity
	}{
		{
			name:      "serie vacia no revienta",
			readings:  nil,
			wantCount: 0,
		},
		{
			name:      "serie con pocos puntos no marca nada",
			readings:  mkSeries(80, 80.2, 79.8),
			wantCount: 0,
		},
		{
			name: "serie estable sin outliers",
			readings: mkSeries(
				80.0, 80.5, 79.3, 80.8, 79.6, 80.2, 81.0, 79.4,
				80.3, 79.7, 80.6, 79.9, 80.4, 79.8,
			),
			wantCount: 0,
		},
		{
			name: "pico claro al final",
			readings: mkSeries(
				80.0, 80.5, 79.3, 80.8, 79.6, 80.2, 81.0, 79.4, 80.3, 79.7,
				75.0,
			),
			wantCount:    1,
			wantIdx:      []int{10},
			wantSeverity: []Severity{SeverityHigh},
		},
		{
			name: "mediciones inestables, dos outliers",
			readings: mkSeries(
				80.0, 80.5, 79.3, 80.8, 79.6, 80.2, 81.0, 79.4, 80.3, 79.7,
				90.0, // bien afuera
				80.1,
				70.0, // bien afuera abajo
			),
			wantCount:    2,
			wantIdx:      []int{10, 12},
			wantSeverity: []Severity{SeverityHigh, SeverityHigh},
		},
		{
			name: "serie con stddev 0 no marca nada",
			readings: mkSeries(
				80, 80, 80, 80, 80, 80, 80, 80,
				81,
			),
			wantCount: 0,
		},
		{
			// Baseline: media ~80, stddev ~0.5. Outlier ~82 => |z| ~ 4 => high.
			// Ajustamos a un valor en el bucket medium a proposito.
			name: "outlier cae en medium cuando esta entre 2.5 y 3.5 sigmas",
			readings: mkSeries(
				80.0, 80.5, 79.3, 80.8, 79.6, 80.2, 81.0, 79.4, 80.3, 79.7,
				81.9, // ~2.8 sigmas arriba contra baseline [79.3, 81.0]
			),
			wantCount:    1,
			wantIdx:      []int{10},
			wantSeverity: []Severity{SeverityMedium},
		},
		{
			name: "fuera de la ventana la lectura no cuenta para el baseline",
			readings: []Reading{
				{At: baseTime, Value: 60.0},
				{At: baseTime.AddDate(0, 0, 90), Value: 80.0},
				{At: baseTime.AddDate(0, 0, 91), Value: 80.5},
				{At: baseTime.AddDate(0, 0, 92), Value: 79.3},
				{At: baseTime.AddDate(0, 0, 93), Value: 80.8},
				{At: baseTime.AddDate(0, 0, 94), Value: 79.6},
				{At: baseTime.AddDate(0, 0, 95), Value: 80.2},
				{At: baseTime.AddDate(0, 0, 96), Value: 81.0},
				{At: baseTime.AddDate(0, 0, 97), Value: 95.0},
			},
			wantCount:    1,
			wantIdx:      []int{8},
			wantSeverity: []Severity{SeverityHigh},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			det, err := New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got := det.Detect(tc.readings)
			if len(got) != tc.wantCount {
				t.Fatalf("len(anomalies) = %d, want %d. got=%+v", len(got), tc.wantCount, got)
			}
			for i, idx := range tc.wantIdx {
				expectAt := tc.readings[idx].At
				if !got[i].At.Equal(expectAt) {
					t.Errorf("anomalia %d en %v, esperaba %v", i, got[i].At, expectAt)
				}
				if got[i].Severity != tc.wantSeverity[i] {
					t.Errorf("anomalia %d severity %q, esperaba %q", i, got[i].Severity, tc.wantSeverity[i])
				}
			}
		})
	}
}

func TestDetect_NoFuturePeek(t *testing.T) {
	// Garantizamos que un outlier no se agarra con puntos del futuro.
	// Si fuera asi, al cargar una lectura mas adelante cambiaria la marca
	// retroactivamente y eso es la receta para un dashboard que mentira.
	t.Parallel()
	det, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Primer punto en la serie, no se puede calificar porque no hay historia.
	readings := []Reading{
		{At: baseTime, Value: 150.0},
	}
	for i := 1; i < 10; i++ {
		readings = append(readings, Reading{
			At:    baseTime.AddDate(0, 0, i),
			Value: 80.0 + float64(i)*0.1,
		})
	}

	anomalies := det.Detect(readings)
	for _, a := range anomalies {
		if a.At.Equal(baseTime) {
			t.Fatalf("no deberiamos marcar el primer punto, no hay historia. got %+v", a)
		}
	}
}

func TestDetect_OrderIndependent(t *testing.T) {
	// Pasar la misma serie desordenada tiene que devolver lo mismo que
	// pasarla ordenada. Lo testeamos porque Detect hace copia defensiva
	// y sort, queremos ese contrato firme.
	t.Parallel()
	det, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ordered := mkSeries(
		80.0, 80.1, 80.0, 79.9, 80.1, 80.0, 80.2, 80.1, 80.0, 79.9,
		95.0,
	)
	shuffled := []Reading{
		ordered[10], ordered[0], ordered[5], ordered[2], ordered[8],
		ordered[1], ordered[9], ordered[3], ordered[7], ordered[4], ordered[6],
	}

	a := det.Detect(ordered)
	b := det.Detect(shuffled)

	if len(a) != len(b) {
		t.Fatalf("resultados distintos len: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !a[i].At.Equal(b[i].At) || math.Abs(a[i].ZScore-b[i].ZScore) > 1e-9 {
			t.Errorf("mismatch i=%d: a=%+v b=%+v", i, a[i], b[i])
		}
	}
}

func TestDetect_DoesNotMutateInput(t *testing.T) {
	// Si Detect ordena el slice del caller, un test que use el slice
	// despues se vuelve loco. Asi que chequeamos la copia defensiva.
	t.Parallel()
	det, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	readings := []Reading{
		{At: baseTime.AddDate(0, 0, 3), Value: 80.0},
		{At: baseTime.AddDate(0, 0, 1), Value: 80.5},
		{At: baseTime.AddDate(0, 0, 2), Value: 79.5},
	}
	beforeOrder := []time.Time{readings[0].At, readings[1].At, readings[2].At}
	_ = det.Detect(readings)
	for i, want := range beforeOrder {
		if !readings[i].At.Equal(want) {
			t.Fatalf("Detect modifico el slice del caller en i=%d", i)
		}
	}
}

func TestNew_ValidatesConfig(t *testing.T) {
	t.Parallel()

	bad := []struct {
		name string
		cfg  Config
	}{
		{"window negativa", Config{WindowDays: -1, MinSamples: 5, LowZ: 2, MediumZ: 2.5, HighZ: 3.5}},
		{"min samples 1", Config{WindowDays: 28, MinSamples: 1, LowZ: 2, MediumZ: 2.5, HighZ: 3.5}},
		{"low z <= 0", Config{WindowDays: 28, MinSamples: 5, LowZ: 0, MediumZ: 2.5, HighZ: 3.5}},
		{"medium <= low", Config{WindowDays: 28, MinSamples: 5, LowZ: 2.5, MediumZ: 2.5, HighZ: 3.5}},
		{"high <= medium", Config{WindowDays: 28, MinSamples: 5, LowZ: 2, MediumZ: 3, HighZ: 2.9}},
	}
	for _, tc := range bad {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tc.cfg); err == nil {
				t.Fatal("esperaba error, got nil")
			}
		})
	}

	// Config buena pasa.
	if _, err := New(DefaultConfig()); err != nil {
		t.Fatalf("default config deberia validar, got %v", err)
	}
}

func TestSeverity_AbsoluteValue(t *testing.T) {
	// Mismo |z| positivo y negativo tienen que mapear a la misma severidad,
	// un pico para arriba es tan sospechoso como uno para abajo.
	t.Parallel()
	d := NewUnchecked(DefaultConfig())
	if d.severity(3.0) != d.severity(-3.0) {
		t.Fatalf("severity asimetrica: pos=%v neg=%v", d.severity(3.0), d.severity(-3.0))
	}
}
