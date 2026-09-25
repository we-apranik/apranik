package config

import "fmt"

type missingFieldError struct {
	field string
	owner string
}

func errMissingField(field, owner string) error {
	return missingFieldError{field: field, owner: owner}
}

func (e missingFieldError) Error() string {
	return fmt.Sprintf("a %s needs a %q field", e.owner, e.field)
}

type unknownOutputError struct{ name string }

func (e unknownOutputError) Error() string {
	return fmt.Sprintf("unknown log output %q; want stderr, file, memory or syslog", e.name)
}
