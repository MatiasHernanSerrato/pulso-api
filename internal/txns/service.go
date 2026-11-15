// Package txns es el service layer de transactions y categorias.
// Contiene la validacion de dominio y el storage. El resolver de graphql
// y el worker de mercadopago llaman a este paquete; ninguno de los dos
// deberia conocer los detalles del storage.
//
// La separacion sirve para: 1) cuando migremos de in-memory a sqlc, solo
// cambia este paquete; 2) tests de dominio no necesitan levantar graphql;
// 3) el worker de mp y cualquier futuro import de csv comparten las mismas
// reglas que la API publica.
package txns

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Kind replica el enum del schema a nivel dominio. Lo tenemos aca y no
// importamos graph/model para evitar la dependencia ciclica (graph importa
// este paquete).
type Kind string

const (
	KindExpense  Kind = "EXPENSE"
	KindIncome   Kind = "INCOME"
	KindTransfer Kind = "TRANSFER"
)

// IsValid chequea que el string sea uno de los kinds conocidos.
func (k Kind) IsValid() bool {
	switch k {
	case KindExpense, KindIncome, KindTransfer:
		return true
	}
	return false
}

// Category es el modelo de categoria. OwnerID nil significa categoria
// global de Pulso.
type Category struct {
	ID       string
	OwnerID  *string
	Name     string
	Kind     Kind
	Icon     *string
	Color    *string
	Archived bool
}

// Transaction es un movimiento de plata.
type Transaction struct {
	ID             string
	UserID         string
	CategoryID     *string
	OccurredAt     time.Time
	Amount         float64
	Currency       string
	Kind           Kind
	Note           *string
	ExternalSource *string
	ExternalRef    *string
}

// Inputs - los expone el service porque son API de dominio, no del schema.

type NewTransaction struct {
	UserID     string
	CategoryID *string
	OccurredAt time.Time
	Amount     float64
	Currency   string
	Kind       Kind
	Note       *string
}

type NewCategory struct {
	OwnerID *string
	Name    string
	Kind    Kind
	Icon    *string
	Color   *string
}

type UpdateCategoryIn struct {
	ID    string
	Name  *string
	Icon  *string
	Color *string
}

type ListFilter struct {
	UserID     string
	From       *time.Time
	To         *time.Time
	CategoryID *string
	Limit      int
}

// Errores de dominio. Se chequean con errors.Is.
var (
	ErrInvalidAmount    = errors.New("amount tiene que ser != 0")
	ErrInvalidKind      = errors.New("transaction kind invalido")
	ErrInvalidCurrency  = errors.New("currency tiene que ser codigo iso de 3 letras")
	ErrCategoryNotFound = errors.New("category no existe o no es tuya")
	ErrEmptyName        = errors.New("name vacio")
	ErrDuplicateName    = errors.New("ya tenes una categoria con ese nombre")
	ErrNotFound         = errors.New("not found")
)

// Service junta todo. Por ahora guarda en memoria (el map adentro).
// Cuando cableemos sqlc, la interface queda igual.
type Service struct {
	mu           sync.RWMutex
	transactions map[string]*Transaction
	categories   map[string]*Category
}

// New arma el service con las categorias default seedeadas, replicando
// lo que va a hacer la migration cuando toque meter el seed real.
func New() *Service {
	s := &Service{
		transactions: make(map[string]*Transaction),
		categories:   make(map[string]*Category),
	}
	s.seedDefaults()
	return s
}

// DefaultCategoryNames devuelve los nombres de las categorias globales,
// util para tests que piden "la lista oficial".
func DefaultCategoryNames() []string {
	return []string{
		"Comida", "Super", "Transporte", "Casa", "Salud", "Ocio",
		"Educacion", "Otros", "Sueldo", "Freelance", "Reintegro", "Transferencia",
	}
}

func (s *Service) seedDefaults() {
	seed := []struct {
		name  string
		kind  Kind
		icon  string
		color string
	}{
		{"Comida", KindExpense, "utensils", "#f97316"},
		{"Super", KindExpense, "shopping-cart", "#ef4444"},
		{"Transporte", KindExpense, "bus", "#3b82f6"},
		{"Casa", KindExpense, "home", "#8b5cf6"},
		{"Salud", KindExpense, "heart-pulse", "#ec4899"},
		{"Ocio", KindExpense, "music", "#10b981"},
		{"Educacion", KindExpense, "book", "#06b6d4"},
		{"Otros", KindExpense, "more-horizontal", "#6b7280"},
		{"Sueldo", KindIncome, "wallet", "#22c55e"},
		{"Freelance", KindIncome, "briefcase", "#84cc16"},
		{"Reintegro", KindIncome, "refresh-cw", "#14b8a6"},
		{"Transferencia", KindTransfer, "arrow-left-right", "#64748b"},
	}
	for _, c := range seed {
		icon := c.icon
		color := c.color
		cat := &Category{
			ID:    uuid.NewString(),
			Name:  c.name,
			Kind:  c.kind,
			Icon:  &icon,
			Color: &color,
		}
		s.categories[cat.ID] = cat
	}
}

// Create inserta una transaction. Valida monto, kind y currency, y
// resuelve la categoria si vino en el input.
func (s *Service) Create(_ context.Context, in NewTransaction) (*Transaction, error) {
	if in.Amount == 0 {
		return nil, ErrInvalidAmount
	}
	if !in.Kind.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidKind, in.Kind)
	}
	cur := strings.TrimSpace(strings.ToUpper(in.Currency))
	if cur == "" {
		cur = "ARS"
	}
	if len(cur) != 3 {
		return nil, fmt.Errorf("%w: %q", ErrInvalidCurrency, in.Currency)
	}
	occurredAt := in.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if in.CategoryID != nil {
		c, ok := s.categories[*in.CategoryID]
		if !ok || c.Archived || (c.OwnerID != nil && *c.OwnerID != in.UserID) {
			return nil, ErrCategoryNotFound
		}
	}

	t := &Transaction{
		ID:         uuid.NewString(),
		UserID:     in.UserID,
		CategoryID: in.CategoryID,
		OccurredAt: occurredAt,
		Amount:     NormalizeAmount(in.Amount, in.Kind),
		Currency:   cur,
		Kind:       in.Kind,
		Note:       in.Note,
	}
	s.transactions[t.ID] = t
	return t, nil
}

// Delete borra una transaction verificando que sea del user.
func (s *Service) Delete(_ context.Context, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.transactions[id]
	if !ok || t.UserID != userID {
		return ErrNotFound
	}
	delete(s.transactions, id)
	return nil
}

// Get devuelve una transaction del user.
func (s *Service) Get(_ context.Context, userID, id string) (*Transaction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.transactions[id]
	if !ok || t.UserID != userID {
		return nil, ErrNotFound
	}
	return t, nil
}

// List devuelve las transactions del user con filtros. Mas recientes primero.
func (s *Service) List(_ context.Context, f ListFilter) ([]*Transaction, error) {
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Transaction, 0)
	for _, t := range s.transactions {
		if t.UserID != f.UserID {
			continue
		}
		if !matchesFilter(t, f) {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListCategories lista las categorias accesibles para el user: globales
// + propias no archivadas, filtradas opcionalmente por kind.
func (s *Service) ListCategories(_ context.Context, userID string, kind *Kind) ([]*Category, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Category, 0, len(s.categories))
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

// CreateCategory crea una categoria para el user.
func (s *Service) CreateCategory(_ context.Context, userID string, in NewCategory) (*Category, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, ErrEmptyName
	}
	if !in.Kind.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidKind, in.Kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.categories {
		if c.Archived {
			continue
		}
		if c.OwnerID != nil && *c.OwnerID == userID && strings.EqualFold(c.Name, name) {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateName, name)
		}
	}
	owner := userID
	cat := &Category{
		ID:      uuid.NewString(),
		OwnerID: &owner,
		Name:    name,
		Kind:    in.Kind,
		Icon:    in.Icon,
		Color:   in.Color,
	}
	s.categories[cat.ID] = cat
	return cat, nil
}

// UpdateCategory edita una categoria del user. No permite tocar globales.
func (s *Service) UpdateCategory(_ context.Context, userID string, in UpdateCategoryIn) (*Category, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.categories[in.ID]
	if !ok || c.OwnerID == nil || *c.OwnerID != userID {
		return nil, ErrCategoryNotFound
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, ErrEmptyName
		}
		c.Name = name
	}
	if in.Icon != nil {
		c.Icon = in.Icon
	}
	if in.Color != nil {
		c.Color = in.Color
	}
	return c, nil
}

// ArchiveCategory soft-deletea.
func (s *Service) ArchiveCategory(_ context.Context, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.categories[id]
	if !ok || c.OwnerID == nil || *c.OwnerID != userID {
		return ErrCategoryNotFound
	}
	c.Archived = true
	return nil
}

// GetCategory es util para hidratar el campo Category del Transaction.
func (s *Service) GetCategory(_ context.Context, id string) (*Category, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.categories[id]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

func matchesFilter(t *Transaction, f ListFilter) bool {
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

// NormalizeAmount deja el signo coherente con el kind.
// Lo exponemos porque el worker de mercadopago lo necesita antes de
// construir el input del Create.
func NormalizeAmount(amount float64, kind Kind) float64 {
	switch kind {
	case KindExpense:
		if amount > 0 {
			return -amount
		}
	case KindIncome:
		if amount < 0 {
			return -amount
		}
	}
	return amount
}
