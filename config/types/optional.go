package types

import "errors"

func ValueOrDefault[T any](written *T, defaultValue T) T {
	if written == nil {
		return defaultValue
	}
	return *written
}

func Pointer[T any](value T) *T { return &value }

var errNotPositive = errors.New("must be greater than zero; leave it out for the default")

func CheckPositive[T ~int | ~int64](written *T) error {
	if written != nil && *written <= 0 {
		return errNotPositive
	}
	return nil
}
