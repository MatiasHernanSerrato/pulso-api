package alerts

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Store es la salida del worker: cualquier backend que sepa persistir
// una alerta y dedupear. El implementation real contra postgres vive
// en internal/db cuando lo cableemos.
type Store interface {
	Insert(ctx context.Context, a Alert) error
}

// Signal es lo que entra al worker: una señal interna que origina una
// posible alerta. Ej: el detector de anomalies produce signals de kind
// weight_anomaly con payload del z-score.
type Signal struct {
	Alert      Alert
	ReceivedAt time.Time
}

// Worker agarra Signals de un canal interno y las empuja al Store.
// Pensado como singleton por proceso; para escalar horizontal vamos
// a tener que pasar a un worker pool con lock en db, pero eso mas adelante.
type Worker struct {
	store Store

	// buffer soft: si llenamos aca, el Push bloquea hasta que un worker
	// drene. En prod lo dejamos en 256 que para el load actual sobra.
	in chan Signal

	// Metrics del worker. Antes estaban detras de un sync.Mutex pero
	// el Stats() leia sin tomar el lock, lo que daba -race warnings
	// en los tests. Las pasamos a atomic.Int64, mas liviano y correcto.
	inserted   atomic.Int64
	duplicates atomic.Int64
	failures   atomic.Int64

	// Lifecycle: cerramos done cuando Run sale para que Stop pueda
	// esperar a que el worker termine de drenar lo que esta procesando.
	startOnce sync.Once
	done      chan struct{}
}

// NewWorker arma el worker con un buffer dado.
func NewWorker(store Store, buffer int) *Worker {
	if buffer <= 0 {
		buffer = 256
	}
	return &Worker{
		store: store,
		in:    make(chan Signal, buffer),
		done:  make(chan struct{}),
	}
}

// Push encola una alerta. Bloqueante si el buffer esta lleno. No usa
// ctx porque los callers de Push no quieren cancelarlo normalmente.
func (w *Worker) Push(s Signal) {
	w.in <- s
}

// Run arranca el loop. Se corta cuando ctx se cancela, drenando antes
// lo que ya tenia en el canal para que las alertas aceptadas no se pierdan.
func (w *Worker) Run(ctx context.Context) {
	w.startOnce.Do(func() {
		go w.loop(ctx)
	})
	<-w.done
}

func (w *Worker) loop(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			// drena lo que quedo encolado antes de salir.
			for {
				select {
				case sig := <-w.in:
					w.handle(context.Background(), sig)
				default:
					return
				}
			}
		case sig := <-w.in:
			w.handle(ctx, sig)
		}
	}
}

// handle procesa una signal y actualiza los counters atomicamente.
func (w *Worker) handle(ctx context.Context, s Signal) {
	err := w.store.Insert(ctx, s.Alert)
	switch {
	case err == nil:
		w.inserted.Add(1)
	case isDup(err):
		w.duplicates.Add(1)
	default:
		log.Printf("alerts worker: insert fallo kind=%s user=%s err=%v",
			s.Alert.Kind, s.Alert.UserID, err)
		w.failures.Add(1)
	}
}

// Stats devuelve snapshot de los counters. Thread-safe.
func (w *Worker) Stats() (inserted, duplicates, failures int64) {
	return w.inserted.Load(), w.duplicates.Load(), w.failures.Load()
}

// isDup separa el error de duplicado del resto. Lo dejamos como var a
// nivel package para poder reescribirlo en tests sin importar errors.
var isDup = func(err error) bool {
	// Comparamos con == a proposito: queremos la sentinela exacta.
	return err == ErrDuplicate
}
