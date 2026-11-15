// Tests de integracion de la capa graph. Ejercen Query y Mutation
// completos, con auth context real, para confirmar que los resolvers
// hablen bien con el store en memoria y respeten @auth.
//
// Cuando migremos a sqlc, esta misma suite debe pasar contra un postgres
// de test (via testcontainers o un docker local). Por eso los tests no
// miran la estructura interna del Resolver mas alla de lo publico.

package graph

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MatiasHernanSerrato/pulso-api/graph/model"
	"github.com/MatiasHernanSerrato/pulso-api/internal/auth"
)

// ctxForUser arma un context con el user id dado como si viniera del
// middleware de auth. Mas limpio que fabricar un signer para cada test.
func ctxForUser(t *testing.T, id uuid.UUID) context.Context {
	t.Helper()
	return auth.WithUser(context.Background(), id)
}

func TestQueryPing_NoAuthRequired(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	got, err := r.Query().Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping err: %v", err)
	}
	if got != "pong" {
		t.Fatalf("Ping=%q, want pong", got)
	}
}

func TestWeights_RequireAuth(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	_, err := r.Query().Weights(context.Background(), nil)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Weights sin auth: got %v, want ErrUnauthenticated", err)
	}
}

func TestCreateWeight_And_ReadBack(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	user := uuid.New()
	ctx := ctxForUser(t, user)

	note := "post gym"
	bf := 18.5
	input := model.CreateWeightInput{
		Kg:         80.3,
		BodyFatPct: &bf,
		Note:       &note,
		Source:     model.WeightSourceBLE,
	}
	w, err := r.Mutation().CreateWeight(ctx, input)
	if err != nil {
		t.Fatalf("CreateWeight: %v", err)
	}
	if w.UserID != user.String() {
		t.Fatalf("weight.UserID=%s, want %s", w.UserID, user)
	}
	if w.Kg != 80.3 {
		t.Fatalf("kg=%v, want 80.3", w.Kg)
	}

	list, err := r.Query().Weights(ctx, nil)
	if err != nil {
		t.Fatalf("Weights query: %v", err)
	}
	if len(list) != 1 || list[0].ID != w.ID {
		t.Fatalf("list=%+v, want solo el que acabamos de crear", list)
	}

	got, err := r.Query().Weight(ctx, w.ID)
	if err != nil {
		t.Fatalf("Weight by id: %v", err)
	}
	if got.ID != w.ID {
		t.Fatalf("Weight.ID=%s, want %s", got.ID, w.ID)
	}
}

func TestCreateWeight_RejectsInvalid(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	ctx := ctxForUser(t, uuid.New())

	cases := []struct {
		name string
		kg   float64
	}{
		{"cero", 0},
		{"negativo", -10},
		{"gigante", 600},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := r.Mutation().CreateWeight(ctx, model.CreateWeightInput{Kg: tc.kg})
			if !errors.Is(err, ErrInvalidWeight) {
				t.Fatalf("got %v, want ErrInvalidWeight", err)
			}
		})
	}
}

func TestDeleteWeight_OnlyOwner(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	alice := uuid.New()
	bob := uuid.New()
	aliceCtx := ctxForUser(t, alice)
	bobCtx := ctxForUser(t, bob)

	w, err := r.Mutation().CreateWeight(aliceCtx, model.CreateWeightInput{Kg: 70.0})
	if err != nil {
		t.Fatalf("CreateWeight: %v", err)
	}

	// bob no puede ni borrar ni ver.
	if _, err := r.Mutation().DeleteWeight(bobCtx, w.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob delete alice weight: %v, want ErrNotFound", err)
	}
	if _, err := r.Query().Weight(bobCtx, w.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bob read alice weight: %v, want ErrNotFound", err)
	}

	ok, err := r.Mutation().DeleteWeight(aliceCtx, w.ID)
	if err != nil {
		t.Fatalf("alice delete: %v", err)
	}
	if !ok {
		t.Fatal("alice delete devolvio false")
	}
	if _, err := r.Query().Weight(aliceCtx, w.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("despues del delete deberia ser not found, got %v", err)
	}
}

func TestWeightsFilter_FromTo(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	user := uuid.New()
	ctx := ctxForUser(t, user)

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		at := now.AddDate(0, 0, -i)
		if _, err := r.Mutation().CreateWeight(ctx, model.CreateWeightInput{
			Kg:         80 + float64(i)*0.1,
			MeasuredAt: &at,
		}); err != nil {
			t.Fatalf("seed i=%d: %v", i, err)
		}
	}

	from := now.AddDate(0, 0, -2)
	to := now
	list, err := r.Query().Weights(ctx, &model.WeightsFilter{From: &from, To: &to})
	if err != nil {
		t.Fatalf("Weights filtered: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len=%d, want 3 (3 dias dentro del rango)", len(list))
	}
	// Orden: mas recientes primero.
	for i := 1; i < len(list); i++ {
		if list[i-1].MeasuredAt.Before(list[i].MeasuredAt) {
			t.Fatalf("orden incorrecto en %d vs %d", i-1, i)
		}
	}
}

func TestWeightsFilter_Limit(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	user := uuid.New()
	ctx := ctxForUser(t, user)

	for i := 0; i < 10; i++ {
		if _, err := r.Mutation().CreateWeight(ctx, model.CreateWeightInput{Kg: 80 + float64(i)*0.1}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	limit := 3
	list, err := r.Query().Weights(ctx, &model.WeightsFilter{Limit: &limit})
	if err != nil {
		t.Fatalf("Weights limit: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("len=%d, want 3", len(list))
	}
}

func TestCategories_SeededDefaults(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	ctx := ctxForUser(t, uuid.New())

	cats, err := r.Query().Categories(ctx, nil)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(cats) == 0 {
		t.Fatal("se esperaban categorias globales por defecto, got 0")
	}

	// Al filtrar por INCOME deben aparecer solo las de INCOME.
	kind := model.TransactionKindIncome
	incCats, err := r.Query().Categories(ctx, &kind)
	if err != nil {
		t.Fatalf("Categories INCOME: %v", err)
	}
	for _, c := range incCats {
		if c.Kind != model.TransactionKindIncome {
			t.Fatalf("categoria %s con kind=%s cuando filtre por INCOME", c.Name, c.Kind)
		}
	}
}

func TestCreateTransaction_And_List(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	user := uuid.New()
	ctx := ctxForUser(t, user)

	note := "cafe"
	tx, err := r.Mutation().CreateTransaction(ctx, model.CreateTransactionInput{
		Amount: 2500,
		Kind:   model.TransactionKindExpense,
		Note:   &note,
	})
	if err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}
	if tx.UserID != user.String() {
		t.Fatalf("tx.UserID=%s, want %s", tx.UserID, user)
	}

	list, err := r.Query().Transactions(ctx, nil)
	if err != nil {
		t.Fatalf("Transactions: %v", err)
	}
	if len(list) != 1 || list[0].ID != tx.ID {
		t.Fatalf("list=%+v, want 1 tx que acabamos de crear", list)
	}
}

func TestTransaction_Isolation(t *testing.T) {
	t.Parallel()
	r := NewResolver()
	alice := uuid.New()
	bob := uuid.New()
	aliceCtx := ctxForUser(t, alice)
	bobCtx := ctxForUser(t, bob)

	_, err := r.Mutation().CreateTransaction(aliceCtx, model.CreateTransactionInput{
		Amount: 1000,
		Kind:   model.TransactionKindExpense,
	})
	if err != nil {
		t.Fatalf("CreateTransaction alice: %v", err)
	}

	bobList, err := r.Query().Transactions(bobCtx, nil)
	if err != nil {
		t.Fatalf("Transactions bob: %v", err)
	}
	if len(bobList) != 0 {
		t.Fatalf("bob ve %d transactions ajenas", len(bobList))
	}
}
