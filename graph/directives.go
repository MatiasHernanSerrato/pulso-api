package graph

import (
	"context"
	"errors"

	"github.com/MatiasHernanSerrato/pulso-api/internal/auth"
)

// ErrUnauthenticated es el error que devolvemos cuando una query o mutation
// marcada con @auth se ejecuta sin un token valido en el context.
// Cuando metamos gqlgen generate esto se mapea a extensions.code = UNAUTHENTICATED.
var ErrUnauthenticated = errors.New("unauthenticated")

// AuthDirective implementa la directiva @auth del schema.
// gqlgen (una vez generado) le va a pasar next como la resolucion del field
// y lo invoca si el guard pasa. Mientras tanto exponemos la funcion
// RequireUser que los resolvers escritos a mano pueden llamar directo.
//
// Firma compatible con gqlgen: func(ctx, obj, next) (res, err).
// next es graphql.Resolver pero lo dejamos como any para no acoplar
// al generated.go que todavia no existe.
func AuthDirective(ctx context.Context, _ any, next func(context.Context) (any, error)) (any, error) {
	if _, err := auth.UserFromContext(ctx); err != nil {
		return nil, ErrUnauthenticated
	}
	return next(ctx)
}

// RequireUser es un guard para resolvers escritos a mano.
// Se usa al principio del resolver para cortar temprano si no hay user.
// Devuelve el uuid del user o el error que hay que retornar tal cual.
func RequireUser(ctx context.Context) (string, error) {
	id, err := auth.UserFromContext(ctx)
	if err != nil {
		return "", ErrUnauthenticated
	}
	return id.String(), nil
}
