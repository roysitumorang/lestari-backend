// Package services holds the business rules; each service works through repositories and is given the
// transaction by its caller.
package services

import "github.com/roysitumorang/lestari-backend/helper"

// percentAtMost100 rejects a percentage above 100 for a type/value pair (discounts, fees, MDR, commissions).
func percentAtMost100(field, kind string, value float64) error {
	if kind == "percent" && value > 100 {
		return helper.Invalid(field, "A percentage cannot be more than 100.")
	}
	return nil
}

func deref[T any](p *T) (zero T) {
	if p == nil {
		return zero
	}
	return *p
}
