// Package alerts modela las alertas que genera Pulso: outliers de peso,
// presupuestos excedidos, pagos de MP sin conciliar, etc. Este archivo
// tiene solo los tipos; el worker vive al lado y los Store adapters
// contra postgres viven en internal/db cuando los cableemos.
package alerts

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Kind clasifica que tipo de alerta es. Nuevos kinds requieren actualizar
// el CHECK constraint de la migration 0003.
type Kind string

const (
	KindWeightAnomaly    Kind = "weight_anomaly"
	KindBudgetExceeded   Kind = "budget_exceeded"
	KindMPReconcileFail  Kind = "mp_reconcile_failed"
	KindBLEDrift         Kind = "ble_drift"
)

// Severity cuan urgente es. "critical" se usa solo para cosas que
// realmente queremos que vibren el celular del usuario.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Status es el ciclo de vida que maneja el usuario del lado del cliente.
type Status string

const (
	StatusOpen         Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusDismissed    Status = "dismissed"
	StatusResolved     Status = "resolved"
)

// Alert es el modelo de dominio. Lo que persistimos en db casi mapea 1:1
// salvo que aca mantenemos payload como map tipado.
type Alert struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Kind        Kind
	Severity    Severity
	Payload     map[string]any
	DedupeKey   *string
	Status      Status
	TriggeredAt time.Time
	ResolvedAt  *time.Time
}

// ErrDuplicate se devuelve cuando el store detecta que ya existe una alert
// con el mismo (user_id, dedupe_key) y esta en open o acknowledged.
// El worker lo trata como "ok, no reencolar".
var ErrDuplicate = errors.New("alert duplicada")

// ErrInvalid se devuelve cuando el input no cumple validacion antes de
// llegar al store. No usamos db CHECK para esto porque queremos el error
// como errors.Is y no un driver error random.
var ErrInvalid = errors.New("alert invalida")
