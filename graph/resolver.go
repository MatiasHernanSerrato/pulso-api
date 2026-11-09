package graph

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/MatiasHernanSerrato/pulso-api/graph/model"
)

// Resolver es el root resolver de GraphQL.
// Por ahora guarda todo en memoria, cuando tengamos la db real lo cambiamos.
type Resolver struct {
	mu      sync.RWMutex
	weights map[string]*model.Weight
}

// NewResolver crea un resolver con storage en memoria.
func NewResolver() *Resolver {
	return &Resolver{
		weights: make(map[string]*model.Weight),
	}
}

// QueryResolver agrupa los resolvers de la raiz Query.
// gqlgen genera esta interface despues, por ahora la definimos a mano.
type QueryResolver interface {
	Ping(ctx context.Context) (string, error)
	Weights(ctx context.Context, filter *model.WeightsFilter) ([]*model.Weight, error)
	Weight(ctx context.Context, id string) (*model.Weight, error)
}

// Query devuelve el query resolver.
func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }

type queryResolver struct{ *Resolver }

// Ping healthcheck basico del schema.
func (r *queryResolver) Ping(_ context.Context) (string, error) {
	return "pong", nil
}

// Weights devuelve todas las mediciones que matchean el filtro.
// Solo del usuario del token: nunca mezclamos data entre users.
// TODO: cuando este la db real, mover esto a una query sqlc.
func (r *queryResolver) Weights(ctx context.Context, filter *model.WeightsFilter) ([]*model.Weight, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	limit := 50
	if filter != nil && filter.Limit != nil && *filter.Limit > 0 {
		limit = *filter.Limit
	}

	var out []*model.Weight
	for _, w := range r.weights {
		if w.UserID != userID {
			continue
		}
		if !matchesFilter(w, filter) {
			continue
		}
		out = append(out, w)
	}

	// mas recientes primero
	sort.Slice(out, func(i, j int) bool {
		return out[i].MeasuredAt.After(out[j].MeasuredAt)
	})

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Weight busca una medicion por id, chequeando que sea del user del token.
func (r *queryResolver) Weight(ctx context.Context, id string) (*model.Weight, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	w, ok := r.weights[id]
	if !ok || w.UserID != userID {
		return nil, ErrNotFound
	}
	return w, nil
}

// ErrNotFound lo devolvemos cuando un resource no existe.
// Despues lo mapeamos a un error de GraphQL con codigo propio.
var ErrNotFound = errors.New("not found")

func matchesFilter(w *model.Weight, f *model.WeightsFilter) bool {
	if f == nil {
		return true
	}
	if f.From != nil && w.MeasuredAt.Before(*f.From) {
		return false
	}
	if f.To != nil && w.MeasuredAt.After(*f.To) {
		return false
	}
	return true
}

