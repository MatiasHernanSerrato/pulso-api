package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// bearerPrefix es el prefijo esperado del header Authorization.
// Lo chequeamos case-insensitive porque hay clientes molestos.
const bearerPrefix = "Bearer "

// Middleware devuelve un chi/stdlib middleware que valida el bearer token
// y mete el user id en el context. Si el token no esta o es invalido
// responde 401 con un json chico, nunca pasa al handler siguiente.
//
// Nota: este middleware es para rutas obligadas. Para rutas mixtas (algunas
// publicas, otras privadas) conviene usar Optional + un guard en el resolver.
func Middleware(s *Signer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := claimsFromRequest(s, r)
			if err != nil {
				writeAuthError(w, err)
				return
			}
			id, err := claims.UserID()
			if err != nil {
				writeAuthError(w, ErrBadSubject)
				return
			}
			ctx := WithUser(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Optional es como Middleware pero no corta la cadena si el token falta
// o es invalido. Solo mete el user en el context cuando el token es valido.
// Util para endpoints de GraphQL donde algunas queries son publicas.
func Optional(s *Signer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := claimsFromRequest(s, r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			id, err := claims.UserID()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := WithUser(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// claimsFromRequest extrae el token del header y lo valida.
// No distinguimos "no header" vs "header raro" porque desde afuera dan 401 igual.
func claimsFromRequest(s *Signer, r *http.Request) (*Claims, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return nil, ErrInvalidToken
	}
	// strings.HasPrefix es case-sensitive, nos conviene aceptar "bearer " tambien.
	if len(h) < len(bearerPrefix) || !strings.EqualFold(h[:len(bearerPrefix)], bearerPrefix) {
		return nil, ErrInvalidToken
	}
	raw := strings.TrimSpace(h[len(bearerPrefix):])
	return s.Parse(raw)
}

// errorBody es la forma del json que devolvemos cuando el auth falla.
type errorBody struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// writeAuthError devuelve el error como 401 con un body chico.
// El code es util en el cliente para distinguir "renova el token" de "mandate loguear".
func writeAuthError(w http.ResponseWriter, err error) {
	code := "invalid_token"
	switch {
	case errors.Is(err, ErrExpiredToken):
		code = "expired_token"
	case errors.Is(err, ErrWrongIssuer):
		code = "wrong_issuer"
	case errors.Is(err, ErrBadSubject):
		code = "bad_subject"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="pulso-api"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(errorBody{Error: err.Error(), Code: code})
}
