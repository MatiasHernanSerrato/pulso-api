package webhooks

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// MPWorker es el consumidor real que esta en el otro lado del EventSink.
// Cuando llega un evento valido del webhook de MP, el worker:
//  1. Llama a la API de MP con el data.id para traer el detalle del payment.
//  2. Infiere la categoria (por ahora solo "Otros", despues metemos matcher).
//  3. Escribe el row en transactions con external_source="mercadopago" +
//     external_ref=<payment_id>, para que el unique index parcial dedupee.
//
// Esto es TODO en varios lugares. Por ahora encola en memoria y loguea,
// sirve para arrancar el cable end-to-end y despues completar el pipeline.
type MPWorker struct {
	// Client es el cliente a la MP API que levanta el detalle del payment.
	// TODO: lo cablo cuando este el paquete internal/mpclient con el access
	// token por user. Mientras tanto queda nil y el worker loguea el evento.
	Client MPClient

	// Store es donde escribimos la transaction una vez tenemos el detalle.
	// TODO: arrancamos con un adapter contra el service de transactions
	// (cuando este, el service vive en internal/txns o parecido).
	Store MPTransactionStore

	// MaxRetries define cuantas veces reintentamos antes de mandar el evento
	// a DLQ (dead letter queue). Por ahora no hay DLQ, solo loguea feo.
	MaxRetries int

	// BackoffBase es el delay inicial del exponential backoff.
	BackoffBase time.Duration

	mu    sync.Mutex
	queue []queuedEvent
}

type queuedEvent struct {
	ctx     EventContext
	payload []byte
	tries   int
}

// MPClient es la interface chica contra la API de MP. Solo pedimos el
// metodo que necesitamos para no acoplar a todo el SDK. El impl real
// va en internal/mpclient (todavia no existe).
type MPClient interface {
	GetPayment(ctx context.Context, paymentID string) (MPPayment, error)
}

// MPPayment es el subset del detalle del payment que nos interesa.
// Hay como 40 campos mas en el payload real, levantamos lo util.
type MPPayment struct {
	ID             string
	UserID         string
	Status         string
	StatusDetail   string
	Amount         float64
	Currency       string
	PaymentMethod  string
	Description    string
	DateApproved   time.Time
	ExternalRef    string
}

// MPTransactionStore es la salida: por donde el worker empuja la
// transaction final. El impl real vive en el service de txns.
type MPTransactionStore interface {
	// UpsertFromMP crea o actualiza una transaction asociada al payment.
	// La idempotencia la da el combo (user, external_source, external_ref)
	// que choca contra el unique index parcial de db/migrations/0002.
	UpsertFromMP(ctx context.Context, userID string, p MPPayment) error
}

// NewMPWorker arma el worker con defaults razonables. Pasar nil en
// Client/Store esta ok por ahora: el worker loguea y encola nomas.
func NewMPWorker(client MPClient, store MPTransactionStore) *MPWorker {
	return &MPWorker{
		Client:      client,
		Store:       store,
		MaxRetries:  5,
		BackoffBase: 2 * time.Second,
	}
}

// Enqueue satisface EventSink para que el handler HTTP le pase los eventos.
// Hoy solo loguea y guarda el payload. Cuando este el pipeline, aca arranca
// una goroutine o lo manda a un worker pool externo.
func (w *MPWorker) Enqueue(ctx EventContext, payload []byte) error {
	w.mu.Lock()
	w.queue = append(w.queue, queuedEvent{ctx: ctx, payload: payload})
	size := len(w.queue)
	w.mu.Unlock()

	log.Printf("mp worker: queued type=%s id=%s user=%s depth=%d",
		ctx.EventType, ctx.EventID, ctx.UserRef, size)

	// TODO: cuando este MPClient y MPTransactionStore cableados, llamar
	// a w.processOne(ctx, payload) en una goroutine con ctx cancelable.
	return nil
}

// Depth devuelve cuantos eventos estan en cola. Lo uso en tests y en el
// endpoint de metricas que viene mas adelante.
func (w *MPWorker) Depth() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.queue)
}

// ErrNotReady se devuelve cuando llaman a ProcessOne sin tener Client o
// Store seteados. Es explicito para que no quede en log "nil pointer".
var ErrNotReady = errors.New("mp worker: client o store sin configurar")

// ProcessOne saca un evento de la cola y corre el pipeline. Hoy es stub:
// necesita Client y Store, sino devuelve ErrNotReady.
//
// TODO: implementar bien cuando esten las dependencias. Flujo esperado:
//   1) parse ev.data.id del payload (ya lo sabemos por EventContext.EventID
//      mas el detalle que viene en ev.data).
//   2) w.Client.GetPayment(ctx, paymentID).
//   3) mapear payment.user_id MP -> user_id Pulso (tabla de links todavia
//      no existe, es el siguiente laburo).
//   4) w.Store.UpsertFromMP(ctx, userID, payment).
//   5) si falla y tries < MaxRetries, re-encolar con backoff exponencial.
func (w *MPWorker) ProcessOne(ctx context.Context) error {
	w.mu.Lock()
	if len(w.queue) == 0 {
		w.mu.Unlock()
		return nil
	}
	ev := w.queue[0]
	w.queue = w.queue[1:]
	w.mu.Unlock()

	if w.Client == nil || w.Store == nil {
		// no abortamos la ejecucion: logueamos y descartamos. Cuando esten
		// cableados esta rama no se toca.
		log.Printf("mp worker: evento descartado (deps nil) type=%s id=%s",
			ev.ctx.EventType, ev.ctx.EventID)
		return ErrNotReady
	}

	// XXX pipeline real va aca, falta.
	_ = ctx
	return nil
}
