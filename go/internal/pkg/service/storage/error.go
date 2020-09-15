package storage

import (
	"errors"
	"fmt"
)

type ErrorCode int

const (
	NotFound ErrorCode = iota
	PreconditionFailed
)

func ErrorIsCode(err error, code ErrorCode) bool {
	var hasCode hasCode
	if errors.As(err, &hasCode) {
		return hasCode.code() == code
	}
	return false
}

type errorWithCode struct {
	c ErrorCode
	s string
}

var _ hasCode = (*errorWithCode)(nil)

func (e *errorWithCode) code() ErrorCode {
	return e.c
}

func (e *errorWithCode) Error() string {
	return e.s
}

type errorWithCodeUnwrap struct {
	c   ErrorCode
	s   string
	err error
}

var _ hasCode = (*errorWithCodeUnwrap)(nil)
var _ unwrap = (*errorWithCodeUnwrap)(nil)

func (e *errorWithCodeUnwrap) code() ErrorCode {
	return e.c
}

func (e *errorWithCodeUnwrap) Error() string {
	return e.s
}

func (e *errorWithCodeUnwrap) Unwrap() error {
	return e.err
}

type hasCode interface {
	code() ErrorCode
}

func NewErrorf(code ErrorCode, format string, args ...interface{}) error {
	err := fmt.Errorf(format, args...)
	if errUnwrap, ok := err.(unwrap); ok {
		return &errorWithCodeUnwrap{
			c:   code,
			s:   err.Error(),
			err: errUnwrap.Unwrap(),
		}
	}
	return &errorWithCode{
		c: code,
		s: err.Error(),
	}
}

type unwrap interface {
	Unwrap() error
}
