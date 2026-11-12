package webhooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// capturingSink guarda lo que se encola asi los tests pueden chequearlo.
type capturingSink struct {
	mu       sync.Mutex
	captured []EventContext
	err      error
}

func (c *capturingSink) Enqueue(ctx EventContext, _ []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.captured = append(c.captured, ctx)
	return nil
}

func signMP(secret, dataID, requestID string, ts int64) string {
	manifest := "id:" + dataID + ";request-id:" + requestID + ";ts:" + strconv.FormatInt(ts, 10) + ";"
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write([]byte(manifest))
	return "ts=" + strconv.FormatInt(ts, 10) + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

func TestMercadoPagoHandler_OK(t *testing.T) {
	now := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	sink := &capturingSink{}
	h := NewMercadoPagoHandler("muy-secreto-dev", sink)
	h.Now = func() time.Time { return now }

	ts := now.UnixMilli()
	body := []byte(`{"id":"evt_1","type":"payment","action":"payment.created","live_mode":false,"user_id":"42","data":{"id":"123"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago?data.id=123", bytes.NewReader(body))
	req.Header.Set("x-signature", signMP("muy-secreto-dev", "123", "req-xyz", ts))
	req.Header.Set("x-request-id", "req-xyz")

	rr := httptest.NewRecorder()
	h.Handler()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if len(sink.captured) != 1 {
		t.Fatalf("esperaba 1 encolado, hay %d", len(sink.captured))
	}
	ev := sink.captured[0]
	if ev.EventID != "evt_1" || ev.EventType != "payment" || ev.UserRef != "42" {
		t.Errorf("event metadata mal: %+v", ev)
	}
}

func TestMercadoPagoHandler_InvalidSignature(t *testing.T) {
	now := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	sink := &capturingSink{}
	h := NewMercadoPagoHandler("muy-secreto-dev", sink)
	h.Now = func() time.Time { return now }

	ts := now.UnixMilli()
	// firma con otro secret
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago?data.id=123",
		bytes.NewReader([]byte(`{"id":"evt_1","type":"payment"}`)))
	req.Header.Set("x-signature", signMP("otro-secret", "123", "req-xyz", ts))
	req.Header.Set("x-request-id", "req-xyz")

	rr := httptest.NewRecorder()
	h.Handler()(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
	if len(sink.captured) != 0 {
		t.Errorf("no deberia encolar en firma invalida, hay %d", len(sink.captured))
	}
}

func TestMercadoPagoHandler_MissingSignature(t *testing.T) {
	sink := &capturingSink{}
	h := NewMercadoPagoHandler("muy-secreto-dev", sink)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago?data.id=123",
		bytes.NewReader([]byte(`{}`)))

	rr := httptest.NewRecorder()
	h.Handler()(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestMercadoPagoHandler_NoSecretReturns503(t *testing.T) {
	h := NewMercadoPagoHandler("", NoopSink{})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	h.Handler()(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rr.Code)
	}
}

func TestMercadoPagoHandler_ReplayWindow(t *testing.T) {
	now := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	sink := &capturingSink{}
	h := NewMercadoPagoHandler("muy-secreto-dev", sink)
	h.Now = func() time.Time { return now }
	h.TolerateClockSkew = 2 * time.Minute

	// evento firmado hace 10 minutos
	old := now.Add(-10 * time.Minute).UnixMilli()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago?data.id=123",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("x-signature", signMP("muy-secreto-dev", "123", "req-xyz", old))
	req.Header.Set("x-request-id", "req-xyz")

	rr := httptest.NewRecorder()
	h.Handler()(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 por ts viejo, got %d", rr.Code)
	}
}

func TestMercadoPagoHandler_Dedup(t *testing.T) {
	now := time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)
	sink := &capturingSink{}
	h := NewMercadoPagoHandler("muy-secreto-dev", sink)
	h.Now = func() time.Time { return now }

	ts := now.UnixMilli()
	body := []byte(`{"id":"evt_dup","type":"payment","data":{"id":"123"}}`)

	send := func() int {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/mercadopago?data.id=123", bytes.NewReader(body))
		req.Header.Set("x-signature", signMP("muy-secreto-dev", "123", "req-same", ts))
		req.Header.Set("x-request-id", "req-same")
		rr := httptest.NewRecorder()
		h.Handler()(rr, req)
		return rr.Code
	}

	if code := send(); code != http.StatusOK {
		t.Fatalf("primera: want 200, got %d", code)
	}
	if code := send(); code != http.StatusOK {
		t.Fatalf("segunda (dup): want 200, got %d", code)
	}
	if len(sink.captured) != 1 {
		t.Errorf("dedup fallo: encolo %d veces", len(sink.captured))
	}
}

func TestParseSignature(t *testing.T) {
	cases := []struct {
		in, ts, mac string
	}{
		{"ts=1,v1=abc", "1", "abc"},
		{"ts=1, v1=abc", "1", "abc"},
		{"v1=abc,ts=1", "1", "abc"},
		{"ts=,v1=abc", "", "abc"},
		{"", "", ""},
	}
	for _, c := range cases {
		ts, mac := parseSignature(c.in)
		if ts != c.ts || mac != c.mac {
			t.Errorf("parseSignature(%q) = (%q,%q), want (%q,%q)", c.in, ts, mac, c.ts, c.mac)
		}
	}
}

func TestMercadoPagoHandler_WrongMethod(t *testing.T) {
	h := NewMercadoPagoHandler("muy-secreto-dev", NoopSink{})
	req := httptest.NewRequest(http.MethodGet, "/webhooks/mercadopago", nil)
	rr := httptest.NewRecorder()
	h.Handler()(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", rr.Code)
	}
}
