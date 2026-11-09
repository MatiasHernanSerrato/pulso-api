package model

import "time"

// Weight representa una medicion de peso corporal. Espejo del type del schema GraphQL.
// Cuando metamos gqlgen generate esto se va a sobreescribir en models_gen.go, por ahora
// lo mantenemos a mano para poder trabajar los resolvers.
type Weight struct {
	ID         string        `json:"id"`
	UserID     string        `json:"userId"`
	Kg         float64       `json:"kg"`
	BodyFatPct *float64      `json:"bodyFatPct,omitempty"`
	MusclePct  *float64      `json:"musclePct,omitempty"`
	MeasuredAt time.Time     `json:"measuredAt"`
	Source     WeightSource  `json:"source"`
	Note       *string       `json:"note,omitempty"`
}

// WeightSource mapea el enum del schema.
type WeightSource string

const (
	WeightSourceManual         WeightSource = "MANUAL"
	WeightSourceBLE            WeightSource = "BLE"
	WeightSourceHealthKit      WeightSource = "HEALTHKIT"
	WeightSourceHealthConnect  WeightSource = "HEALTH_CONNECT"
)

// AllWeightSources sirve para validar valores que vienen de afuera.
var AllWeightSources = []WeightSource{
	WeightSourceManual,
	WeightSourceBLE,
	WeightSourceHealthKit,
	WeightSourceHealthConnect,
}

// IsValid chequea si el valor es uno de los permitidos.
func (s WeightSource) IsValid() bool {
	switch s {
	case WeightSourceManual, WeightSourceBLE, WeightSourceHealthKit, WeightSourceHealthConnect:
		return true
	}
	return false
}

// String implementa fmt.Stringer.
func (s WeightSource) String() string { return string(s) }

// WeightsFilter es el input del query de weights.
type WeightsFilter struct {
	From  *time.Time `json:"from,omitempty"`
	To    *time.Time `json:"to,omitempty"`
	Limit *int       `json:"limit,omitempty"`
}

// CreateWeightInput es el payload del mutation createWeight.
// MeasuredAt es opcional, si no viene tomamos time.Now en el resolver.
type CreateWeightInput struct {
	Kg         float64      `json:"kg"`
	BodyFatPct *float64     `json:"bodyFatPct,omitempty"`
	MusclePct  *float64     `json:"musclePct,omitempty"`
	MeasuredAt *time.Time   `json:"measuredAt,omitempty"`
	Source     WeightSource `json:"source"`
	Note       *string      `json:"note,omitempty"`
}
