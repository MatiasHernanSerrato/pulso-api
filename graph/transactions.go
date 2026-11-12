package graph

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MatiasHernanSerrato/pulso-api/graph/model"
)

// TxStore es el storage en memoria para transactions mientras no cableamos
// el service real al resolver. Lo tenemos separado del Resolver principal
// para que cuando migremos al service con sqlc sea un cambio localizado.
type TxStore struct {
	mu           sync.RWMutex
	transactions map[string]*model.Transaction
	categories   map[string]*model.Category
	once         sync.Once
}

// NewTxStore arma el store con las categorias default ya cargadas.
// Las categorias default imitan lo que se seedea en la migration,
// asi dev local sin db sigue respondiendo con algo razonable.
func NewTxStore() *TxStore {
	s := &TxStore{
		transactions: make(map[string]*model.Transaction),
		categories:   make(map[string]*model.Category),
	}
	s.seedDefaults()
	return s
}

// DefaultCategoryNames devuelve los nombres de las categorias globales,
// util para ciertos tests que piden "la lista oficial".
func DefaultCategoryNames() []string {
	return []string{
		"Comida", "Super", "Transporte", "Casa", "Salud", "Ocio",
		"Educacion", "Otros", "Sueldo", "Freelance", "Reintegro", "Transferencia",
	}
}

func (s *TxStore) seedDefaults() {
	s.once.Do(func() {
		seed := []struct {
			name  string
			kind  model.TransactionKind
			icon  string
			color string
		}{
			{"Comida", model.TransactionKindExpense, "utensils", "#f97316"},
			{"Super", model.TransactionKindExpense, "shopping-cart", "#ef4444"},
			{"Transporte", model.TransactionKindExpense, "bus", "#3b82f6"},
			{"Casa", model.TransactionKindExpense, "home", "#8b5cf6"},
			{"Salud", model.TransactionKindExpense, "heart-pulse", "#ec4899"},
			{"Ocio", model.TransactionKindExpense, "music", "#10b981"},
			{"Educacion", model.TransactionKindExpense, "book", "#06b6d4"},
			{"Otros", model.TransactionKindExpense, "more-horizontal", "#6b7280"},
			{"Sueldo", model.TransactionKindIncome, "wallet", "#22c55e"},
			{"Freelance", model.TransactionKindIncome, "briefcase", "#84cc16"},
			{"Reintegro", model.TransactionKindIncome, "refresh-cw", "#14b8a6"},
			{"Transferencia", model.TransactionKindTransfer, "arrow-left-right", "#64748b"},
		}
		for _, c := range seed {
			icon := c.icon
			color := c.color
			cat := &model.Category{
				ID:    uuid.NewString(),
				Name:  c.name,
				Kind:  c.kind,
				Icon:  &icon,
				Color: &color,
			}
			s.categories[cat.ID] = cat
		}
	})
}

// Store devuelve el store de transactions, para que los resolvers lo pinchen.
// Lo exponemos solo para tests del paquete graph.
func (r *Resolver) Store() *TxStore { return r.tx }

// Errores de dominio de transactions.
var (
	ErrInvalidAmount    = errors.New("amount tiene que ser != 0")
	ErrInvalidKind      = errors.New("transaction kind invalido")
	ErrInvalidCurrency  = errors.New("currency tiene que ser codigo iso de 3 letras")
	ErrCategoryNotFound = errors.New("category no existe o no es tuya")
)

// Categories lista las categorias disponibles para el user. Las globales
// (sin owner) aparecen siempre, las del user solo si no estan archivadas.
func (r *queryResolver) Categories(ctx context.Context, kind *model.TransactionKind) ([]*model.Category, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	s := r.tx
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*model.Category, 0, len(s.categories))
	for _, c := range s.categories {
		if c.Archived {
			continue
		}
		if c.OwnerID != nil && *c.OwnerID != userID {
			continue
		}
		if kind != nil && c.Kind != *kind {
			continue
		}
		out = append(out, c)
	}
	// globales primero, despues por kind y nombre
	sort.Slice(out, func(i, j int) bool {
		gi := out[i].OwnerID == nil
		gj := out[j].OwnerID == nil
		if gi != gj {
			return gi
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Transactions lista los movimientos del user con filtro opcional.
func (r *queryResolver) Transactions(ctx context.Context, filter *model.TransactionsFilter) ([]*model.Transaction, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	s := r.tx
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := 100
	if filter != nil && filter.Limit != nil && *filter.Limit > 0 {
		limit = *filter.Limit
	}

	out := make([]*model.Transaction, 0)
	for _, t := range s.transactions {
		if t.UserID != userID {
			continue
		}
		if !matchesTxFilter(t, filter) {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].OccurredAt.After(out[j].OccurredAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Transaction devuelve una transaction del user.
func (r *queryResolver) Transaction(ctx context.Context, id string) (*model.Transaction, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	s := r.tx
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.transactions[id]
	if !ok || t.UserID != userID {
		return nil, ErrNotFound
	}
	return t, nil
}

// CreateTransaction es el mutation que valida input y guarda.
func (r *mutationResolver) CreateTransaction(ctx context.Context, input model.CreateTransactionInput) (*model.Transaction, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	if input.Amount == 0 {
		return nil, ErrInvalidAmount
	}
	if !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidKind, input.Kind)
	}
	cur := strings.TrimSpace(strings.ToUpper(input.Currency))
	if cur == "" {
		cur = "ARS"
	}
	if len(cur) != 3 {
		return nil, fmt.Errorf("%w: %q", ErrInvalidCurrency, input.Currency)
	}

	occurredAt := time.Now().UTC()
	if input.OccurredAt != nil {
		occurredAt = input.OccurredAt.UTC()
	}

	s := r.tx
	s.mu.Lock()
	defer s.mu.Unlock()

	// si el user mando categoryId, chequeamos que exista y sea accesible
	if input.CategoryID != nil {
		c, ok := s.categories[*input.CategoryID]
		if !ok || c.Archived || (c.OwnerID != nil && *c.OwnerID != userID) {
			return nil, ErrCategoryNotFound
		}
	}

	t := &model.Transaction{
		ID:         uuid.NewString(),
		UserID:     userID,
		CategoryID: input.CategoryID,
		OccurredAt: occurredAt,
		Amount:     normalizeAmount(input.Amount, input.Kind),
		Currency:   cur,
		Kind:       input.Kind,
		Note:       input.Note,
	}
	s.transactions[t.ID] = t
	return t, nil
}

// DeleteTransaction borra una transaction del user.
func (r *mutationResolver) DeleteTransaction(ctx context.Context, id string) (bool, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return false, err
	}
	s := r.tx
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.transactions[id]
	if !ok || t.UserID != userID {
		return false, ErrNotFound
	}
	delete(s.transactions, id)
	return true, nil
}

// CreateCategory guarda una categoria custom del user.
func (r *mutationResolver) CreateCategory(ctx context.Context, input model.CreateCategoryInput) (*model.Category, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("name vacio")
	}
	if !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidKind, input.Kind)
	}

	s := r.tx
	s.mu.Lock()
	defer s.mu.Unlock()

	// no permitimos duplicar nombre (case-insensitive) dentro del user
	for _, c := range s.categories {
		if c.Archived {
			continue
		}
		if c.OwnerID != nil && *c.OwnerID == userID && strings.EqualFold(c.Name, name) {
			return nil, fmt.Errorf("ya tenes una categoria %q", name)
		}
	}

	owner := userID
	cat := &model.Category{
		ID:      uuid.NewString(),
		OwnerID: &owner,
		Name:    name,
		Kind:    input.Kind,
		Icon:    input.Icon,
		Color:   input.Color,
	}
	s.categories[cat.ID] = cat
	return cat, nil
}

// UpdateCategory cambia datos de una categoria del user. Solo se pueden
// tocar las propias, nunca las globales.
func (r *mutationResolver) UpdateCategory(ctx context.Context, input model.UpdateCategoryInput) (*model.Category, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	s := r.tx
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.categories[input.ID]
	if !ok || c.OwnerID == nil || *c.OwnerID != userID {
		return nil, ErrCategoryNotFound
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, errors.New("name vacio")
		}
		c.Name = name
	}
	if input.Icon != nil {
		c.Icon = input.Icon
	}
	if input.Color != nil {
		c.Color = input.Color
	}
	return c, nil
}

// ArchiveCategory soft-deletea una categoria del user.
func (r *mutationResolver) ArchiveCategory(ctx context.Context, id string) (bool, error) {
	userID, err := RequireUser(ctx)
	if err != nil {
		return false, err
	}
	s := r.tx
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.categories[id]
	if !ok || c.OwnerID == nil || *c.OwnerID != userID {
		return false, ErrCategoryNotFound
	}
	c.Archived = true
	return true, nil
}

func matchesTxFilter(t *model.Transaction, f *model.TransactionsFilter) bool {
	if f == nil {
		return true
	}
	if f.From != nil && t.OccurredAt.Before(*f.From) {
		return false
	}
	if f.To != nil && t.OccurredAt.After(*f.To) {
		return false
	}
	if f.CategoryID != nil {
		if t.CategoryID == nil || *t.CategoryID != *f.CategoryID {
			return false
		}
	}
	return true
}

// normalizeAmount deja el signo coherente con el kind.
// expense -> negativo, income -> positivo, transfer -> como venga.
// Asi las queries de suma por kind no necesitan ABS() ni WHERE por signo.
func normalizeAmount(amount float64, kind model.TransactionKind) float64 {
	switch kind {
	case model.TransactionKindExpense:
		if amount > 0 {
			return -amount
		}
	case model.TransactionKindIncome:
		if amount < 0 {
			return -amount
		}
	}
	return amount
}
