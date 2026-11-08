// Package weights expone operaciones de alto nivel sobre la tabla weights.
// El resolver de graphql llama aca y nunca directamente a sqlc.
package weights

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MatiasHernanSerrato/pulso-api/internal/db"
	"github.com/MatiasHernanSerrato/pulso-api/internal/db/sqlcgen"
)

// Source define de donde vino la medicion. No lo tomamos del cliente
// a ciegas, lo validamos aca.
type Source string

const (
	SourceManual         Source = "manual"
	SourceBLE            Source = "ble"
	SourceHealthKit      Source = "healthkit"
	SourceHealthConnect  Source = "health_connect"
)

// Weight es el modelo que viaja por la capa de servicio. Tiene menos
// ruido que el generado por sqlc (ej. nada de pgtype.Numeric en la interfaz).
type Weight struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	MeasuredAt time.Time
	WeightKg   float64
	BodyFat    *float64
	Source     Source
	Note       *string
	CreatedAt  time.Time
}

// NewWeight es lo que se pasa al crear. Separado de Weight porque
// no queremos que el cliente setee ID o CreatedAt.
type NewWeight struct {
	UserID     uuid.UUID
	MeasuredAt time.Time
	WeightKg   float64
	BodyFat    *float64
	Source     Source
	Note       *string
}

// ErrInvalid se devuelve cuando los valores no pasan validacion propia
// (la db tiene CHECK constraints pero mejor fallar antes).
var ErrInvalid = errors.New("invalid weight")

// ErrNotFound cuando no existe.
var ErrNotFound = errors.New("weight not found")

type Service struct {
	q sqlcgen.Querier
}

// New arma un service a partir del pool.
func New(pool *db.Pool) *Service {
	return &Service{q: sqlcgen.New(pool.Pool)}
}

// NewWithQuerier util para tests, inyectando un mock.
func NewWithQuerier(q sqlcgen.Querier) *Service {
	return &Service{q: q}
}

// Create inserta una medicion. Normaliza valores y valida rango.
func (s *Service) Create(ctx context.Context, in NewWeight) (*Weight, error) {
	if err := validateKg(in.WeightKg); err != nil {
		return nil, err
	}
	if in.BodyFat != nil {
		if err := validateBodyFat(*in.BodyFat); err != nil {
			return nil, err
		}
	}
	if in.Source == "" {
		in.Source = SourceManual
	}
	if !isKnownSource(in.Source) {
		return nil, fmt.Errorf("%w: source %q", ErrInvalid, in.Source)
	}
	if in.MeasuredAt.IsZero() {
		in.MeasuredAt = time.Now().UTC()
	}

	kg, err := numericFromFloat(in.WeightKg)
	if err != nil {
		return nil, err
	}
	bf, err := optNumericFromFloat(in.BodyFat)
	if err != nil {
		return nil, err
	}

	row, err := s.q.InsertWeight(ctx, sqlcgen.InsertWeightParams{
		UserID:     in.UserID,
		MeasuredAt: pgtype.Timestamptz{Time: in.MeasuredAt, Valid: true},
		WeightKg:   kg,
		BodyFat:    bf,
		Source:     string(in.Source),
		Note:       pgText(in.Note),
	})
	if err != nil {
		return nil, fmt.Errorf("insert: %w", err)
	}
	return rowToWeight(row.ID, row.UserID, row.MeasuredAt, row.WeightKg, row.BodyFat, row.Source, row.Note, row.CreatedAt)
}

// Get devuelve una medicion. Chequea que pertenezca al user para no filtrar.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (*Weight, error) {
	row, err := s.q.GetWeight(ctx, sqlcgen.GetWeightParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgtype.ErrScanTargetTypeChanged) || isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rowToWeight(row.ID, row.UserID, row.MeasuredAt, row.WeightKg, row.BodyFat, row.Source, row.Note, row.CreatedAt)
}

// Delete borra una medicion. Idempotente desde el punto de vista del caller.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return s.q.DeleteWeight(ctx, sqlcgen.DeleteWeightParams{ID: id, UserID: userID})
}

// ListParams filtros para listar.
type ListParams struct {
	UserID uuid.UUID
	From   time.Time
	To     time.Time
	Limit  int32
}

// List devuelve mediciones dentro de un rango, mas recientes primero.
func (s *Service) List(ctx context.Context, p ListParams) ([]*Weight, error) {
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 100
	}
	if p.From.IsZero() {
		p.From = time.Now().AddDate(-1, 0, 0)
	}
	if p.To.IsZero() {
		p.To = time.Now().Add(24 * time.Hour)
	}

	rows, err := s.q.ListWeightsByUser(ctx, sqlcgen.ListWeightsByUserParams{
		UserID:  p.UserID,
		FromTs:  pgtype.Timestamptz{Time: p.From, Valid: true},
		ToTs:    pgtype.Timestamptz{Time: p.To, Valid: true},
		MaxRows: p.Limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]*Weight, 0, len(rows))
	for _, r := range rows {
		w, err := rowToWeight(r.ID, r.UserID, r.MeasuredAt, r.WeightKg, r.BodyFat, r.Source, r.Note, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// Latest devuelve la ultima medicion del user, util para la home screen.
func (s *Service) Latest(ctx context.Context, userID uuid.UUID) (*Weight, error) {
	row, err := s.q.LatestWeight(ctx, userID)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rowToWeight(row.ID, row.UserID, row.MeasuredAt, row.WeightKg, row.BodyFat, row.Source, row.Note, row.CreatedAt)
}

func validateKg(kg float64) error {
	if kg <= 0 || kg >= 500 {
		return fmt.Errorf("%w: weight_kg %g fuera de rango (0, 500)", ErrInvalid, kg)
	}
	return nil
}

func validateBodyFat(bf float64) error {
	if bf < 0 || bf > 100 {
		return fmt.Errorf("%w: body_fat %g fuera de rango [0, 100]", ErrInvalid, bf)
	}
	return nil
}

func isKnownSource(s Source) bool {
	switch s {
	case SourceManual, SourceBLE, SourceHealthKit, SourceHealthConnect:
		return true
	}
	return false
}
