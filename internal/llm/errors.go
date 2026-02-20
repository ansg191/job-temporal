package llm

import (
	"errors"
	"fmt"
)

type ConfigError struct {
	msg string
}

func (e *ConfigError) Error() string {
	return e.msg
}

func NewConfigError(format string, args ...any) error {
	return &ConfigError{msg: fmt.Sprintf(format, args...)}
}

func IsConfigError(err error) bool {
	var cfgErr *ConfigError
	return errors.As(err, &cfgErr)
}
