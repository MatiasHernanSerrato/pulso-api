package alerts

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeStore es un Store minimo para tests: cuenta inserts y opcionalmente
// devuelve ErrDuplicate o un error arbitrario segun el modo.
type fakeStore struct {
	mu        sync.Mutex
	inserted  int
	mode      string // "", "dup", "err"
	errReturn error
}

func (s *fakeStore) Insert(_ context.Context, _ Alert) error {
	s.mu.Lock()
	s.inserted++
	s.mu.Unlock()
	switch s.mode {
	case "dup":
		return ErrDuplicate
	case "err":
		return s.errReturn
	}
	return nil
}

func (s *fakeStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inserted
}

func mkSignal() Signal {
	return Signal{
		Alert: Alert{
			ID:          uuid.New(),
			UserID:      uuid.New(),
			Kind:        KindWeightAnomaly,
			Severity:    SeverityHigh,
			Payload:     map[string]any{"z": 3.2},
			TriggeredAt: time.Now(),
			Status:      StatusOpen,
		},
		ReceivedAt: time.Now(),
	}
}

// TestWorker_StatsAccessFromGoroutines reproduce la race que teniamos
// antes de atomic: Stats leia los counters que handle escribia sin
// coordinacion. Con -race, el build pre-fix prende la alarma; con atomic
// queda limpio.
func TestWorker_StatsAccessFromGoroutines(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	w := NewWorker(store, 128)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(runDone)
	}()

	// Un productor que empuja signals.
	go func() {
		for i := 0; i < 200; i++ {
			w.Push(mkSignal())
		}
	}()

	// Un lector que pide Stats mientras el worker inserta.
	// Si hubiera race sin atomic, -race la pescaria aca.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			_, _, _ = w.Stats()
		}
	}()

	<-readerDone
	// esperamos a que el worker termine de drenar
	for store.count() < 200 && time.Now().Before(time.Now().Add(2*time.Second)) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-runDone

	ins, dup, fail := w.Stats()
	if ins+dup+fail < 200 {
		t.Fatalf("procesados=%d, want 200+. ins=%d dup=%d fail=%d", ins+dup+fail, ins, dup, fail)
	}
}

func TestWorker_CountsByOutcome(t *testing.T) {
	t.Parallel()

	okStore := &fakeStore{}
	dupStore := &fakeStore{mode: "dup"}

	// 50 inserts ok
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	w := NewWorker(okStore, 128)
	go func() {
		w.Run(ctx)
		close(runDone)
	}()
	for i := 0; i < 50; i++ {
		w.Push(mkSignal())
	}
	// esperamos a que drene
	for okStore.count() < 50 {
		time.Sleep(5 * time.Millisecond)
	}
	ins, dup, fail := w.Stats()
	if ins != 50 || dup != 0 || fail != 0 {
		t.Fatalf("after 50 ok: ins=%d dup=%d fail=%d", ins, dup, fail)
	}
	cancel()
	<-runDone

	// 30 duplicates
	ctx2, cancel2 := context.WithCancel(context.Background())
	runDone2 := make(chan struct{})
	w2 := NewWorker(dupStore, 128)
	go func() {
		w2.Run(ctx2)
		close(runDone2)
	}()
	for i := 0; i < 30; i++ {
		w2.Push(mkSignal())
	}
	for dupStore.count() < 30 {
		time.Sleep(5 * time.Millisecond)
	}
	ins, dup, fail = w2.Stats()
	if ins != 0 || dup != 30 || fail != 0 {
		t.Fatalf("after 30 dup: ins=%d dup=%d fail=%d", ins, dup, fail)
	}
	cancel2()
	<-runDone2
}

func TestWorker_DrainsOnShutdown(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	w := NewWorker(store, 128)
	ctx, cancel := context.WithCancel(context.Background())

	// Encolamos 100 antes de arrancar Run, asi cuando cancel() se llame
	// enseguida, el worker aun tiene que drenar lo encolado.
	for i := 0; i < 100; i++ {
		w.Push(mkSignal())
	}

	runDone := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(runDone)
	}()

	// pequeña ventana para que arranque, despues cancelamos
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-runDone

	if store.count() != 100 {
		t.Fatalf("procesados=%d, want 100 (drain completo)", store.count())
	}
}
