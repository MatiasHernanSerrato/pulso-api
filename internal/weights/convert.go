package weights

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// numericFromFloat convierte float64 a pgtype.Numeric sin perder decimales
// por el camino del parse de string, que es lo que recomienda pgx para precision.
func numericFromFloat(f float64) (pgtype.Numeric, error) {
	s := strconv.FormatFloat(f, 'f', 3, 64)
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		return n, fmt.Errorf("numeric scan: %w", err)
	}
	return n, nil
}

func optNumericFromFloat(f *float64) (pgtype.Numeric, error) {
	if f == nil {
		return pgtype.Numeric{Valid: false}, nil
	}
	return numericFromFloat(*f)
}

// floatFromNumeric es lo inverso. Si el numerico es NaN o invalido devolvemos 0
// para que no se cuele un NaN por la api.
func floatFromNumeric(n pgtype.Numeric) (float64, error) {
	if !n.Valid {
		return 0, nil
	}
	if n.NaN {
		return 0, errors.New("numeric NaN")
	}
	// Int * 10^Exp
	bi := new(big.Int).Set(n.Int)
	bf := new(big.Float).SetInt(bi)
	if n.Exp != 0 {
		mul := new(big.Float).SetFloat64(1)
		if n.Exp > 0 {
			ten := big.NewFloat(10)
			for i := int32(0); i < n.Exp; i++ {
				mul.Mul(mul, ten)
			}
			bf.Mul(bf, mul)
		} else {
			ten := big.NewFloat(10)
			for i := int32(0); i < -n.Exp; i++ {
				mul.Mul(mul, ten)
			}
			bf.Quo(bf, mul)
		}
	}
	v, _ := bf.Float64()
	return v, nil
}

func pgText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func textToPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func rowToWeight(
	id uuid.UUID,
	userID uuid.UUID,
	measuredAt pgtype.Timestamptz,
	weight pgtype.Numeric,
	bodyFat pgtype.Numeric,
	source string,
	note pgtype.Text,
	createdAt pgtype.Timestamptz,
) (*Weight, error) {
	kg, err := floatFromNumeric(weight)
	if err != nil {
		return nil, fmt.Errorf("weight: %w", err)
	}
	var bf *float64
	if bodyFat.Valid {
		v, err := floatFromNumeric(bodyFat)
		if err != nil {
			return nil, fmt.Errorf("body_fat: %w", err)
		}
		bf = &v
	}
	return &Weight{
		ID:         id,
		UserID:     userID,
		MeasuredAt: measuredAt.Time,
		WeightKg:   kg,
		BodyFat:    bf,
		Source:     Source(source),
		Note:       textToPtr(note),
		CreatedAt:  createdAt.Time,
	}, nil
}

// isNoRows chequea pgx.ErrNoRows sin que tengamos que importar pgx en todos lados.
func isNoRows(err error) bool {
	return err != nil && err.Error() == "no rows in result set"
}
