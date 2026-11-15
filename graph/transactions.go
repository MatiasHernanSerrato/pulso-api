package graph

import (
	"context"
	"errors"

	"github.com/MatiasHernanSerrato/pulso-api/graph/model"
	"github.com/MatiasHernanSerrato/pulso-api/internal/txns"
)

// Los errores de dominio se re-exportan como aliases asi los tests
// existentes no se rompen. Los resolvers delegan la logica al service.
var (
	ErrInvalidAmount    = txns.ErrInvalidAmount
	ErrInvalidKind      = txns.ErrInvalidKind
	ErrInvalidCurrency  = txns.ErrInvalidCurrency
	ErrCategoryNotFound = txns.ErrCategoryNotFound
)

// DefaultCategoryNames devuelve los nombres de las categorias globales,
// util para ciertos tests que piden "la lista oficial".
func DefaultCategoryNames() []string {
	return txns.DefaultCategoryNames()
}

// Categories lista las categorias disponibles para el user.
func (r *queryResolver) Categories(ctx context.Context, kind *model.TransactionKind) ([]*model.Category, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var k *txns.Kind
	if kind != nil {
		kk := txns.Kind(*kind)
		k = &kk
	}
	list, err := r.tx.ListCategories(ctx, userID, k)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Category, len(list))
	for i, c := range list {
		out[i] = toModelCategory(c)
	}
	return out, nil
}

// Transactions lista los movimientos del user con filtro opcional.
func (r *queryResolver) Transactions(ctx context.Context, filter *model.TransactionsFilter) ([]*model.Transaction, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	f := txns.ListFilter{UserID: userID}
	if filter != nil {
		f.From = filter.From
		f.To = filter.To
		f.CategoryID = filter.CategoryID
		if filter.Limit != nil {
			f.Limit = *filter.Limit
		}
	}
	list, err := r.tx.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Transaction, len(list))
	for i, t := range list {
		out[i] = toModelTx(t)
	}
	return out, nil
}

// Transaction devuelve una transaction del user.
func (r *queryResolver) Transaction(ctx context.Context, id string) (*model.Transaction, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	t, err := r.tx.Get(ctx, userID, id)
	if errors.Is(err, txns.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return toModelTx(t), nil
}

// CreateTransaction valida input y delega al service.
func (r *mutationResolver) CreateTransaction(ctx context.Context, input model.CreateTransactionInput) (*model.Transaction, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	in := txns.NewTransaction{
		UserID:     userID,
		CategoryID: input.CategoryID,
		Amount:     input.Amount,
		Kind:       txns.Kind(input.Kind),
		Note:       input.Note,
	}
	if input.OccurredAt != nil {
		in.OccurredAt = *input.OccurredAt
	}
	in.Currency = input.Currency
	t, err := r.tx.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	return toModelTx(t), nil
}

// DeleteTransaction borra una transaction del user.
func (r *mutationResolver) DeleteTransaction(ctx context.Context, id string) (bool, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return false, err
	}
	if err := r.tx.Delete(ctx, userID, id); err != nil {
		if errors.Is(err, txns.ErrNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}
	return true, nil
}

// CreateCategory guarda una categoria custom del user.
func (r *mutationResolver) CreateCategory(ctx context.Context, input model.CreateCategoryInput) (*model.Category, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	c, err := r.tx.CreateCategory(ctx, userID, txns.NewCategory{
		Name:  input.Name,
		Kind:  txns.Kind(input.Kind),
		Icon:  input.Icon,
		Color: input.Color,
	})
	if err != nil {
		return nil, err
	}
	return toModelCategory(c), nil
}

// UpdateCategory cambia datos de una categoria del user.
func (r *mutationResolver) UpdateCategory(ctx context.Context, input model.UpdateCategoryInput) (*model.Category, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	c, err := r.tx.UpdateCategory(ctx, userID, txns.UpdateCategoryIn{
		ID:    input.ID,
		Name:  input.Name,
		Icon:  input.Icon,
		Color: input.Color,
	})
	if err != nil {
		return nil, err
	}
	return toModelCategory(c), nil
}

// ArchiveCategory soft-deletea una categoria del user.
func (r *mutationResolver) ArchiveCategory(ctx context.Context, id string) (bool, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return false, err
	}
	if err := r.tx.ArchiveCategory(ctx, userID, id); err != nil {
		return false, err
	}
	return true, nil
}

// toModelCategory adapta el tipo del dominio al tipo del schema.
// La duplicacion de structs es el precio a pagar por no acoplar graph
// y service; se gana flexibilidad para evolucionar cada uno.
func toModelCategory(c *txns.Category) *model.Category {
	if c == nil {
		return nil
	}
	return &model.Category{
		ID:       c.ID,
		OwnerID:  c.OwnerID,
		Name:     c.Name,
		Kind:     model.TransactionKind(c.Kind),
		Icon:     c.Icon,
		Color:    c.Color,
		Archived: c.Archived,
	}
}

func toModelTx(t *txns.Transaction) *model.Transaction {
	if t == nil {
		return nil
	}
	return &model.Transaction{
		ID:             t.ID,
		UserID:         t.UserID,
		CategoryID:     t.CategoryID,
		OccurredAt:     t.OccurredAt,
		Amount:         t.Amount,
		Currency:       t.Currency,
		Kind:           model.TransactionKind(t.Kind),
		Note:           t.Note,
		ExternalSource: t.ExternalSource,
		ExternalRef:    t.ExternalRef,
	}
}
