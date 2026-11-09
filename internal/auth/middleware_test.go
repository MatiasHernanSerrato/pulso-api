package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// handlerThatWritesUserID devuelve 200 con el user id si esta en el context,
// o 500 si falta. Lo usamos como "next" en varios tests.
func handlerThatWritesUserID(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := UserFromContext(r.Context())
		if err != nil {
			t.Errorf("middleware deberia haber puesto el user: %v", err)
			http.Error(w, "no user", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(id.String()))
	}
}

func TestMiddleware_ValidTokenAllowsThrough(t *testing.T) {
	s := mustSigner(t, "secret-middleware")
	userID := uuid.New()
	tok, err := s.Generate(userID, "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()

	Middleware(s)(handlerThatWritesUserID(t)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("code: %d, body: %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != userID.String() {
		t.Errorf("body: %q, quiero %q", rr.Body.String(), userID.String())
	}
}

func TestMiddleware_BearerCaseInsensitive(t *testing.T) {
	// Varios clientes mandan "bearer <token>" todo en minuscula, tenemos
	// que aceptarlo para no generar reportes de "no me anda el login".
	s := mustSigner(t, "secret")
	userID := uuid.New()
	tok, _ := s.Generate(userID, "")

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "bearer "+tok)
	rr := httptest.NewRecorder()

	Middleware(s)(handlerThatWritesUserID(t)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("code inesperado: %d", rr.Code)
	}
}

func TestMiddleware_NoHeader_Returns401(t *testing.T) {
	s := mustSigner(t, "secret")

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rr := httptest.NewRecorder()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	Middleware(s)(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code: %d", rr.Code)
	}
	if called {
		t.Fatal("next no deberia haber corrido")
	}
	if got := rr.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("falta header WWW-Authenticate")
	}

	var body errorBody
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("respuesta no es json: %v", err)
	}
	if body.Code != "invalid_token" {
		t.Errorf("code: %q", body.Code)
	}
}

func TestMiddleware_ExpiredTokenReturnsExpiredCode(t *testing.T) {
	s := mustSigner(t, "secret-exp")
	// clock hacia atras para emitir uno vencido
	s.now = func() time.Time { return time.Now().Add(-24 * time.Hour) }
	tok, _ := s.Generate(uuid.New(), "")
	s.now = time.Now

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()

	Middleware(s)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no deberia haber llegado al next")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code: %d", rr.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.Code != "expired_token" {
		t.Errorf("code: %q, quiero expired_token", body.Code)
	}
}

func TestMiddleware_GarbageTokenReturns401(t *testing.T) {
	s := mustSigner(t, "secret-garbage")

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer no-es-un-jwt")
	rr := httptest.NewRecorder()

	Middleware(s)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next no deberia correr con token basura")
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code: %d", rr.Code)
	}
}

func TestMiddleware_HeaderSinBearer(t *testing.T) {
	s := mustSigner(t, "secret-sinbearer")
	tok, _ := s.Generate(uuid.New(), "")

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", tok) // sin "Bearer "
	rr := httptest.NewRecorder()

	called := false
	Middleware(s)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("code: %d", rr.Code)
	}
	if called {
		t.Fatal("next no deberia haber corrido")
	}
}

func TestOptional_WithoutToken_PassesThrough(t *testing.T) {
	s := mustSigner(t, "secret-opt")

	req := httptest.NewRequest(http.MethodGet, "/api/graphql", nil)
	rr := httptest.NewRecorder()

	called := false
	Optional(s)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, err := UserFromContext(r.Context()); !errors.Is(err, ErrNoUser) {
			t.Error("no deberia haber user en el context")
		}
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if !called {
		t.Fatal("Optional deberia haber dejado pasar")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("code: %d", rr.Code)
	}
}

func TestOptional_WithValidToken_SetsUser(t *testing.T) {
	s := mustSigner(t, "secret-opt2")
	userID := uuid.New()
	tok, _ := s.Generate(userID, "")

	req := httptest.NewRequest(http.MethodGet, "/api/graphql", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()

	Optional(s)(handlerThatWritesUserID(t)).ServeHTTP(rr, req)

	if rr.Body.String() != userID.String() {
		t.Errorf("user en context: %q, quiero %q", rr.Body.String(), userID.String())
	}
}

func TestContext_RoundTrip(t *testing.T) {
	ctx := WithUser(ctxTestBase(), uuid.MustParse("00000000-0000-0000-0000-000000000042"))
	id, err := UserFromContext(ctx)
	if err != nil {
		t.Fatalf("UserFromContext: %v", err)
	}
	if id.String() != "00000000-0000-0000-0000-000000000042" {
		t.Errorf("id: %s", id)
	}
}

func TestMustUserFromContext_PanicsWhenMissing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustUserFromContext deberia panic con context vacio")
		}
	}()
	MustUserFromContext(ctxTestBase())
}
