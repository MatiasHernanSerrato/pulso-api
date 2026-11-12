package model

import "time"

// TransactionKind mapea el enum del schema.
type TransactionKind string

const (
	TransactionKindExpense  TransactionKind = "EXPENSE"
	TransactionKindIncome   TransactionKind = "INCOME"
	TransactionKindTransfer TransactionKind = "TRANSFER"
)

// AllTransactionKinds util para validacion.
var AllTransactionKinds = []TransactionKind{
	TransactionKindExpense,
	TransactionKindIncome,
	TransactionKindTransfer,
}

// IsValid chequea si el valor es uno de los permitidos.
func (k TransactionKind) IsValid() bool {
	switch k {
	case TransactionKindExpense, TransactionKindIncome, TransactionKindTransfer:
		return true
	}
	return false
}

// String implementa fmt.Stringer.
func (k TransactionKind) String() string { return string(k) }

// ToLower devuelve la representacion que usa la db (expense/income/transfer).
func (k TransactionKind) ToLower() string {
	switch k {
	case TransactionKindExpense:
		return "expense"
	case TransactionKindIncome:
		return "income"
	case TransactionKindTransfer:
		return "transfer"
	}
	return ""
}

// TransactionKindFromDB levanta el valor almacenado y lo pasa al enum.
func TransactionKindFromDB(s string) TransactionKind {
	switch s {
	case "expense":
		return TransactionKindExpense
	case "income":
		return TransactionKindIncome
	case "transfer":
		return TransactionKindTransfer
	}
	return TransactionKindExpense
}

// Category es el mirror del type GraphQL.
type Category struct {
	ID       string          `json:"id"`
	OwnerID  *string         `json:"ownerId,omitempty"`
	Name     string          `json:"name"`
	Kind     TransactionKind `json:"kind"`
	Icon     *string         `json:"icon,omitempty"`
	Color    *string         `json:"color,omitempty"`
	Archived bool            `json:"archived"`
}

// Transaction es el mirror del type GraphQL.
type Transaction struct {
	ID             string          `json:"id"`
	UserID         string          `json:"userId"`
	CategoryID     *string         `json:"categoryId,omitempty"`
	OccurredAt     time.Time       `json:"occurredAt"`
	Amount         float64         `json:"amount"`
	Currency       string          `json:"currency"`
	Kind           TransactionKind `json:"kind"`
	Note           *string         `json:"note,omitempty"`
	ExternalSource *string         `json:"externalSource,omitempty"`
	ExternalRef    *string         `json:"externalRef,omitempty"`
}

// TransactionsFilter es el input del query de transactions.
type TransactionsFilter struct {
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
	CategoryID *string    `json:"categoryId,omitempty"`
	Limit      *int       `json:"limit,omitempty"`
}

// CreateTransactionInput es el payload del mutation createTransaction.
// El userId no viaja aca, lo saca el resolver del token.
type CreateTransactionInput struct {
	CategoryID *string         `json:"categoryId,omitempty"`
	OccurredAt *time.Time      `json:"occurredAt,omitempty"`
	Amount     float64         `json:"amount"`
	Currency   string          `json:"currency"`
	Kind       TransactionKind `json:"kind"`
	Note       *string         `json:"note,omitempty"`
}

// CreateCategoryInput payload del mutation createCategory.
type CreateCategoryInput struct {
	Name  string          `json:"name"`
	Kind  TransactionKind `json:"kind"`
	Icon  *string         `json:"icon,omitempty"`
	Color *string         `json:"color,omitempty"`
}

// UpdateCategoryInput payload del mutation updateCategory.
// Todos los campos salvo id son opcionales (PATCH semantico).
type UpdateCategoryInput struct {
	ID    string  `json:"id"`
	Name  *string `json:"name,omitempty"`
	Icon  *string `json:"icon,omitempty"`
	Color *string `json:"color,omitempty"`
}
