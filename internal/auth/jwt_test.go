package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func mustSigner(t *testing.T, secret string) *Signer {
	t.Helper()
	s, err := NewSigner(secret)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

func TestNewSigner_RejectsEmptySecret(t *testing.T) {
	if _, err := NewSigner(""); err == nil {
		t.Fatal("esperaba error con secret vacio, no vino nada")
	}
}

func TestGenerateAndParse_RoundTrip(t *testing.T) {
	s := mustSigner(t, "un-secret-de-test-mas-o-menos-largo")
	userID := uuid.New()

	tok, err := s.Generate(userID, "matt@pulso.app")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if tok == "" {
		t.Fatal("token vacio")
	}

	c, err := s.Parse(tok)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Email != "matt@pulso.app" {
		t.Errorf("email no coincide: %q", c.Email)
	}
	got, err := c.UserID()
	if err != nil {
		t.Fatalf("UserID: %v", err)
	}
	if got != userID {
		t.Errorf("user id: got %v, quiero %v", got, userID)
	}
}

func TestGenerate_RejectsNilUser(t *testing.T) {
	s := mustSigner(t, "un-secret")
	if _, err := s.Generate(uuid.Nil, ""); err == nil {
		t.Fatal("esperaba error con uuid.Nil")
	}
}

func TestParse_ExpiredToken(t *testing.T) {
	s := mustSigner(t, "otro-secret-de-tests")
	// forzamos el reloj hacia atras para generar un token ya vencido
	s.now = func() time.Time { return time.Now().Add(-24 * time.Hour) }

	tok, err := s.Generate(uuid.New(), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// restauramos time.Now para que parse use el actual
	s.now = time.Now
	_, err = s.Parse(tok)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("queria ErrExpiredToken, vino: %v", err)
	}
}

func TestParse_WrongSecretFails(t *testing.T) {
	s1 := mustSigner(t, "secret-uno")
	s2 := mustSigner(t, "secret-dos")

	tok, err := s1.Generate(uuid.New(), "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := s2.Parse(tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("queria ErrInvalidToken, vino: %v", err)
	}
}

func TestParse_EmptyTokenFails(t *testing.T) {
	s := mustSigner(t, "secret")
	if _, err := s.Parse(""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("queria ErrInvalidToken, vino: %v", err)
	}
}

func TestParse_RejectsWrongIssuer(t *testing.T) {
	// Armamos un token con el mismo secret pero issuer distinto
	s := mustSigner(t, "mismo-secret")
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "pulso-fake",
			Subject:   uuid.New().String(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(s.secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := s.Parse(signed); !errors.Is(err, ErrWrongIssuer) {
		t.Fatalf("queria ErrWrongIssuer, vino: %v", err)
	}
}

func TestParse_RejectsAlgNone(t *testing.T) {
	// Un atacante clasico: firmar con "none" y esperar que el server lo acepte.
	// Nuestro parser restringe a HS256 por WithValidMethods, asi que no pasa.
	s := mustSigner(t, "mismo-secret")
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}

	_, err = s.Parse(signed)
	if err == nil {
		t.Fatal("token alg=none deberia ser rechazado")
	}
	if !strings.Contains(err.Error(), "invalido") && !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("queria ErrInvalidToken, vino: %v", err)
	}
}

func TestParse_BadSubject(t *testing.T) {
	// subject que no es uuid -> ErrBadSubject
	s := mustSigner(t, "secret-subject")
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   "no-soy-un-uuid",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(s.secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := s.Parse(signed); !errors.Is(err, ErrBadSubject) {
		t.Fatalf("queria ErrBadSubject, vino: %v", err)
	}
}

func TestWithTTL_DoesNotMutateOriginal(t *testing.T) {
	s := mustSigner(t, "secret")
	original := s.ttl
	s2 := s.WithTTL(time.Minute)
	if s.ttl != original {
		t.Error("WithTTL mutó el signer original")
	}
	if s2.ttl != time.Minute {
		t.Error("WithTTL no aplicó el nuevo ttl")
	}
}
