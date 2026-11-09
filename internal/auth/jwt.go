// Package auth se encarga de generar y validar tokens JWT para la api.
// Usamos HS256 con un secret del environment. Nada de RSA por ahora porque
// no tenemos clientes de terceros que necesiten verificar sin llamar al server.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// DefaultTTL es la duracion de un token recien generado.
// Lo elegi corto (12hs) para que un leak no joda tanto. Despues sumamos refresh.
const DefaultTTL = 12 * time.Hour

// Issuer identifica a quien emitio el token. Lo chequeamos al parsear.
const Issuer = "pulso-api"

// Errores que exporta el paquete asi los callers pueden distinguir el caso.
var (
	ErrInvalidToken = errors.New("auth: token invalido")
	ErrExpiredToken = errors.New("auth: token expirado")
	ErrWrongIssuer  = errors.New("auth: issuer incorrecto")
	ErrBadSubject   = errors.New("auth: subject no es un uuid valido")
)

// Claims son los claims custom que mete pulso dentro del JWT.
// Guardamos el user id en Subject (siguiendo la convencion de RFC 7519)
// y le agregamos email para no tener que pegarle a la db en cada request.
type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// Signer encapsula la generacion y validacion de tokens.
// El secret se pasa en el constructor para que el package no dependa
// del paquete config directamente (asi es facil de testear).
type Signer struct {
	secret []byte
	ttl    time.Duration
	// now permite inyectar el tiempo en tests sin tocar time.Now global.
	now func() time.Time
}

// NewSigner crea un Signer con TTL por default (12hs).
// Si el secret esta vacio devuelve error porque no queremos firmar con "".
func NewSigner(secret string) (*Signer, error) {
	if secret == "" {
		return nil, errors.New("auth: secret vacio")
	}
	return &Signer{
		secret: []byte(secret),
		ttl:    DefaultTTL,
		now:    time.Now,
	}, nil
}

// WithTTL devuelve una copia del signer con otro TTL.
// Util para tokens de recuperacion de password, que duran menos.
func (s *Signer) WithTTL(ttl time.Duration) *Signer {
	cp := *s
	cp.ttl = ttl
	return &cp
}

// Generate arma un JWT firmado con HS256 para el usuario dado.
// Si email viene vacio no lo mete en los claims, no es obligatorio.
func (s *Signer) Generate(userID uuid.UUID, email string) (string, error) {
	if userID == uuid.Nil {
		return "", errors.New("auth: user id vacio")
	}
	now := s.now()
	claims := Claims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
			NotBefore: jwt.NewNumericDate(now.Add(-1 * time.Second)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("auth: firma: %w", err)
	}
	return signed, nil
}

// Parse valida el token y devuelve los claims.
// Los errores los normalizamos a los que exporta el paquete asi los callers
// no dependen de los tipos internos de jwt/v5.
func (s *Signer) Parse(raw string) (*Claims, error) {
	if raw == "" {
		return nil, ErrInvalidToken
	}
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("metodo de firma inesperado: %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithIssuer(Issuer), jwt.WithValidMethods([]string{"HS256"}))

	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return nil, ErrExpiredToken
		case errors.Is(err, jwt.ErrTokenInvalidIssuer):
			return nil, ErrWrongIssuer
		default:
			return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
		}
	}
	if !tok.Valid {
		return nil, ErrInvalidToken
	}
	// El subject tiene que ser un uuid parseable, si no algo raro paso
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, ErrBadSubject
	}
	return claims, nil
}

// UserID es un helper que parsea el subject de los claims a uuid.UUID.
func (c *Claims) UserID() (uuid.UUID, error) {
	return uuid.Parse(c.Subject)
}
