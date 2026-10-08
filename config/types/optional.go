package types

import "errors"

// A setting a user may leave out is a pointer: nil is "left out" and means the
// default, and anything written is the user's value. Zero never stands for
// the default, so a written zero is refused wherever it makes no sense.

// ValueOrDefault is what the user wrote, or defaultValue when the key was left out.
func ValueOrDefault[T any](written *T, defaultValue T) T {
	if written == nil {
		return defaultValue
	}
	return *written
}

// Pointer makes a written value, for code that builds settings by hand.
func Pointer[T any](value T) *T { return &value }

var errNotPositive = errors.New("must be greater than zero; leave it out for the default")

// CheckPositive passes a setting that was left out and a positive one, and
// refuses a written zero or negative.
func CheckPositive[T ~int | ~int64](written *T) error {
	if written != nil && *written <= 0 {
		return errNotPositive
	}
	return nil
}
