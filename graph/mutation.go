package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/MatiasHernanSerrato/pulso-api/graph/model"
)

// MutationResolver agrupa los resolvers de la raiz Mutation.
// Igual que QueryResolver, cuando corramos gqlgen generate esta interface
// la va a emitir el, mientras tanto la mantenemos a mano para que compile.
type MutationResolver interface {
	CreateWeight(ctx context.Context, input model.CreateWeightInput) (*model.Weight, error)
	DeleteWeight(ctx context.Context, id string) (bool, error)
}

// Mutation devuelve el mutation resolver.
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

type mutationResolver struct{ *Resolver }

// Errores del dominio de weights. Los sacamos del resolver asi los callers
// pueden distinguir el caso sin parsear strings.
var (
	ErrInvalidWeight = errors.New("peso fuera de rango razonable")
	ErrInvalidSource = errors.New("source invalido")
)

// CreateWeight crea una medicion para el usuario del token.
// Guardado en memoria por ahora. Cuando pase a sqlc va a ser un INSERT.
func (r *mutationResolver) CreateWeight(ctx context.Context, input model.CreateWeightInput) (*model.Weight, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	if input.Kg <= 0 || input.Kg >= 500 {
		return nil, fmt.Errorf("%w: kg=%v", ErrInvalidWeight, input.Kg)
	}
	src := input.Source
	if src == "" {
		src = model.WeightSourceManual
	}
	if !src.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidSource, src)
	}

	measuredAt := time.Now().UTC()
	if input.MeasuredAt != nil {
		measuredAt = input.MeasuredAt.UTC()
	}

	w := &model.Weight{
		ID:         uuid.NewString(),
		UserID:     userID,
		Kg:         input.Kg,
		BodyFatPct: input.BodyFatPct,
		MusclePct:  input.MusclePct,
		MeasuredAt: measuredAt,
		Source:     src,
		Note:       input.Note,
	}

	r.mu.Lock()
	r.weights[w.ID] = w
	r.mu.Unlock()

	return w, nil
}

// DeleteWeight borra una medicion del user actual.
// Si la medicion existe pero es de otro user tambien damos not found,
// asi no leakeamos la existencia de ids ajenos.
func (r *mutationResolver) DeleteWeight(ctx context.Context, id string) (bool, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return false, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	w, ok := r.weights[id]
	if !ok || w.UserID != userID {
		return false, ErrNotFound
	}
	delete(r.weights, id)
	return true, nil
}
