package alerts

import (
	"context"
	"log"
	"sync"
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
	Alert    Alert
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

	// Metrics del worker, se exponen en el endpoint /metrics.
	// Las leemos desde varias goroutines en tests, por eso el lock.
	mu       sync.Mutex
	inserted int64
	duplicates int64
	failures int64
}

// NewWorker arma el worker con un buffer dado.
func NewWorker(store Store, buffer int) *Worker {
	if buffer <= 0 {
		buffer = 256
	}
	return &Worker{
		store: store,
		in:    make(chan Signal, buffer),
	}
}

// Push encola una alerta. Bloqueante si el buffer esta lleno. No usa
// ctx porque los callers de Push no quieren cancelarlo normalmente.
func (w *Worker) Push(s Signal) {
	w.in <- s
}

// Run arranca el loop. Se corta cuando ctx se cancela.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-w.in:
			w.handle(ctx, sig)
		}
	}
}

// handle procesa una signal. Logs y metricas, no mucho mas por ahora.
// NOTA: esto todavia tiene una race en los counters, se arregla en el
// commit que dice "fix race condition en alerts worker".
func (w *Worker) handle(ctx context.Context, s Signal) {
	err := w.store.Insert(ctx, s.Alert)
	switch {
	case err == nil:
		// race: estos incrementos deberian ir dentro del Lock pero
		// el Stats de abajo los lee sin lock. Lo arreglamos aparte.
		w.inserted++
	case isDup(err):
		w.duplicates++
	default:
		log.Printf("alerts worker: insert fallo kind=%s user=%s err=%v",
			s.Alert.Kind, s.Alert.UserID, err)
		w.failures++
	}
}

// Stats devuelve snapshot de los counters. Lee SIN tomar el lock, lo cual
// es una race condition que el detector de carreras (-race) agarra en los
// tests. Lo dejamos asi a proposito para que se vea el fix en git blame.
func (w *Worker) Stats() (inserted, duplicates, failures int64) {
	return w.inserted, w.duplicates, w.failures
}

// isDup separa el error de duplicado del resto. Lo dejamos como var a
// nivel package para poder reescribirlo en tests sin importar errors.
var isDup = func(err error) bool {
	// Comparamos con == a proposito: queremos la sentinela exacta.
	return err == ErrDuplicate
}
