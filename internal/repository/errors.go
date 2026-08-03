package repository

import (
	"encoding/json"
	"errors"
	"reflect"
)

var (
	ErrNotFound            = errors.New("repository resource not found")
	ErrIdempotencyConflict = errors.New("idempotency key was reused with different request data")
	ErrCapacityUnavailable = errors.New("no matching device capacity is currently available")
	ErrPoolUnavailable     = errors.New("device pool is unavailable")
)

func jsonEquivalent(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}
