// Package webhooks agrupa los handlers HTTP que recibe pulso-api de
// integraciones externas. El primero es Mercado Pago: cuando el user
// autoriza pulso en MP, cada payment le pega aca y nosotros lo
// convertimos en una transaction del tipo expense/income.
//
// Este archivo es un stub a proposito: valida firma, deduplica, acusa
// recibo y encola el payload. El sync real con transactions va en la
// proxima iteracion (necesita mapping de categorias + llamado a la MP
// API para traer el detalle del payment usando el id del evento).
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MercadoPagoHandler guarda la config necesaria para validar y responder
// webhooks de Mercado Pago. Lo tenemos como struct asi en los tests
// podemos inyectar un clock y un sink distinto.
type MercadoPagoHandler struct {
	// Secret es el webhook secret que MP emite al crear la integracion.
	// Si queda vacio, el handler responde 503 y loguea un warning.
	Secret string

	// MaxBodyBytes limita cuanto leemos del body. 1 MiB es mas de suficiente
	// para los payloads de MP, cualquier cosa arriba de eso es ruido.
	MaxBodyBytes int64

	// TolerateClockSkew es la ventana maxima entre el ts del header y el now
	// del server para aceptar el evento. Protege contra replay de hace rato
	// sin dejar afuera clientes con clock drift tipico.
	TolerateClockSkew time.Duration

	// Now es un hook de clock para tests.
	Now func() time.Time

	// Sink es donde encolamos los eventos validos. En prod lo apuntamos a
	// un worker que llama a la MP API y escribe en transactions. En dev y
	// tests podemos usar un sink in-memory.
	Sink EventSink

	// seenMu + seen implementan dedup en memoria por request.id. Si MP
	// reintenta el mismo evento (lo hace cuando tardamos en responder 200),
	// ya tenemos una respuesta cacheada. Es best effort: cuando este el
	// pipeline real, dedup final la hace el unique index en transactions.
	seenMu sync.Mutex
	seen   map[string]time.Time
}

// EventSink es el consumidor de eventos validos. En prod apunta a un
// worker con retry + llamadas a la API de MP; en dev es un noop.
type EventSink interface {
	Enqueue(ctx EventContext, payload []byte) error
}

// EventContext junta la metadata que armamos antes de encolar.
type EventContext struct {
	EventID   string
	EventType string
	UserRef   string
	LiveMode  bool
	ReceivedAt time.Time
}

// NewMercadoPagoHandler arma el handler con defaults razonables.
// Si secret esta vacio el handler sigue arrancando pero el endpoint
// responde 503 para que se note en el log del lb.
func NewMercadoPagoHandler(secret string, sink EventSink) *MercadoPagoHandler {
	return &MercadoPagoHandler{
		Secret:            secret,
		MaxBodyBytes:      1 << 20,
		TolerateClockSkew: 5 * time.Minute,
		Now:               time.Now,
		Sink:              sink,
		seen:              make(map[string]time.Time),
	}
}

// Handler implementa http.Handler. Validamos firma y luego encolamos.
// El diseño es "acusar recibo rapido": MP reintenta si no ve 200 en pocos
// segundos, asi que todo lo caro va al sink de forma asincrona.
func (h *MercadoPagoHandler) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if h.Secret == "" {
			log.Println("mercadopago webhook llamado sin secret configurado")
			http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, h.MaxBodyBytes))
		if err != nil {
			http.Error(w, "cant read body", http.StatusBadRequest)
			return
		}
		defer func() { _ = r.Body.Close() }()

		sig := r.Header.Get("x-signature")
		reqID := r.Header.Get("x-request-id")
		dataID := r.URL.Query().Get("data.id")

		if err := h.verifySignature(sig, reqID, dataID); err != nil {
			log.Printf("mercadopago firma invalida: %v (request-id=%s)", err, reqID)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		if reqID != "" && h.isDuplicate(reqID) {
			// MP reintentando. Responder 200 asi no sigue reintentando.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"duplicate":true}`))
			return
		}

		var ev mpEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}

		ctx := EventContext{
			EventID:    ev.ID(),
			EventType:  ev.Type,
			UserRef:    ev.UserRef(),
			LiveMode:   ev.LiveMode,
			ReceivedAt: h.Now().UTC(),
		}
		if h.Sink != nil {
			if err := h.Sink.Enqueue(ctx, body); err != nil {
				log.Printf("mercadopago enqueue fallo: %v", err)
				http.Error(w, "enqueue failed", http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

// verifySignature replica el schema x-signature de MP:
//   ts=1234,v1=<hmac_sha256_hex>
// El manifest que se firma es: "id:<data.id>;request-id:<req>;ts:<ts>;"
// Con padding de campo vacio si alguno no viene.
func (h *MercadoPagoHandler) verifySignature(header, requestID, dataID string) error {
	if header == "" {
		return errors.New("falta x-signature")
	}
	ts, mac := parseSignature(header)
	if ts == "" || mac == "" {
		return errors.New("x-signature mal formado")
	}

	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errors.New("ts no es int")
	}
	eventTime := time.UnixMilli(tsInt)
	if eventTime.IsZero() {
		return errors.New("ts fuera de rango")
	}
	diff := h.Now().Sub(eventTime)
	if diff < 0 {
		diff = -diff
	}
	if h.TolerateClockSkew > 0 && diff > h.TolerateClockSkew {
		return errors.New("ts fuera de la ventana")
	}

	manifest := "id:" + dataID + ";request-id:" + requestID + ";ts:" + ts + ";"
	expected := hmacHex(h.Secret, manifest)
	if !hmac.Equal([]byte(expected), []byte(mac)) {
		return errors.New("mac no coincide")
	}
	return nil
}

// isDuplicate deja pasar la primera vez un request.id y marca los
// siguientes como dup por 10 minutos. El GC es inline: mientras
// chequeamos, tiramos las entradas vencidas.
func (h *MercadoPagoHandler) isDuplicate(id string) bool {
	h.seenMu.Lock()
	defer h.seenMu.Unlock()
	now := h.Now()
	// gc rapido
	for k, t := range h.seen {
		if now.Sub(t) > 10*time.Minute {
			delete(h.seen, k)
		}
	}
	if _, ok := h.seen[id]; ok {
		return true
	}
	h.seen[id] = now
	return false
}

// parseSignature parte el header x-signature. Es "k=v,k=v" sin espacios
// pero MP a veces manda espacios despues de la coma, asi que trimmeamos.
func parseSignature(h string) (ts, mac string) {
	for _, kv := range strings.Split(h, ",") {
		kv = strings.TrimSpace(kv)
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(kv[:eq])
		v := strings.TrimSpace(kv[eq+1:])
		switch k {
		case "ts":
			ts = v
		case "v1":
			mac = v
		}
	}
	return
}

func hmacHex(secret, manifest string) string {
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write([]byte(manifest))
	return hex.EncodeToString(m.Sum(nil))
}

// mpEvent es un subset del payload de MP. Solo levantamos lo que nos
// interesa para rutear al worker; el resto lo parsea el worker cuando
// pide el detalle a la API.
type mpEvent struct {
	EventID  string         `json:"id"`
	Type     string         `json:"type"`
	Action   string         `json:"action"`
	LiveMode bool           `json:"live_mode"`
	UserID   json.Number    `json:"user_id"`
	Data     mpEventData    `json:"data"`
	Extra    map[string]any `json:"extra"`
}

type mpEventData struct {
	ID string `json:"id"`
}

func (e mpEvent) ID() string {
	if e.EventID != "" {
		return e.EventID
	}
	return e.Data.ID
}

func (e mpEvent) UserRef() string {
	return e.UserID.String()
}

// NoopSink es un sink que loguea nomas, util para dev y staging mientras
// no hay un worker real.
type NoopSink struct{}

// Enqueue solo loguea el evento. Error nunca.
func (NoopSink) Enqueue(ctx EventContext, _ []byte) error {
	log.Printf("mp webhook (noop): type=%s id=%s user=%s live=%v",
		ctx.EventType, ctx.EventID, ctx.UserRef, ctx.LiveMode)
	return nil
}
