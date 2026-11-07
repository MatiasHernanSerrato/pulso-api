//go:build tools
// +build tools

// Este archivo esta solo para fijar deps de herramientas (gqlgen, sqlc, etc)
// que no participan del build normal. Ver https://github.com/golang/go/issues/25922
package tools

import (
	_ "github.com/99designs/gqlgen"
	_ "github.com/99designs/gqlgen/graphql/introspection"
)
