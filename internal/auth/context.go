package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ctxKey es un tipo propio asi no choca con otras keys del context.
// Regla de oro: nunca usar un string pelado como key de context.
type ctxKey int

const (
	userIDKey ctxKey = iota + 1
)

// ErrNoUser se devuelve cuando se intenta leer el user del context
// pero no esta. Casi siempre significa que te olvidaste del middleware.
var ErrNoUser = errors.New("auth: usuario no encontrado en el context")

// WithUser mete el user id en el context.
// Lo usa el middleware despues de validar el token.
func WithUser(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// UserFromContext devuelve el user id del context.
// Si no esta, devuelve ErrNoUser.
func UserFromContext(ctx context.Context) (uuid.UUID, error) {
	v := ctx.Value(userIDKey)
	if v == nil {
		return uuid.Nil, ErrNoUser
	}
	id, ok := v.(uuid.UUID)
	if !ok {
		return uuid.Nil, ErrNoUser
	}
	return id, nil
}

// MustUserFromContext es la version que panic-ea si no hay user.
// Para usar solo adentro de resolvers protegidos, nunca en codigo publico.
func MustUserFromContext(ctx context.Context) uuid.UUID {
	id, err := UserFromContext(ctx)
	if err != nil {
		panic(err)
	}
	return id
}
