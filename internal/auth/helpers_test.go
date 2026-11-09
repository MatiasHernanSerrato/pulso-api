package auth

import "context"

// ctxTestBase es un helper chico para no repetir context.Background en los tests.
// Esta en su propio archivo porque lo usan jwt_test y middleware_test.
func ctxTestBase() context.Context { return context.Background() }
