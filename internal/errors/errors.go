package errors

import (
	"errors"
	"fmt"
)

type ErrorCode int

const (
	NotFound ErrorCode = iota + 1
	PreconditionFailed
)

func ErrorIsCode(err error, code ErrorCode) bool {
	return GetErrorCode(err) == code
}

type errorStruct struct {
	c   ErrorCode
	s   string
	err error
}

var _ error = (*errorStruct)(nil)
var _ hasCode = (*errorStruct)(nil)

func (e *errorStruct) code() ErrorCode {
	return e.c
}

func (e *errorStruct) Error() string {
	return e.s
}

func (e *errorStruct) Unwrap() error {
	return e.err
}

type hasCode interface {
	code() ErrorCode
}

func NewError(code ErrorCode, s string) error {
	return &errorStruct{
		c: code,
		s: s,
	}
}

func NewErrorf(code ErrorCode, format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	return &errorStruct{
		c:   code,
		s:   err.Error(),
		err: errors.Unwrap(err),
	}
}

func GetErrorCode(err error) ErrorCode {
	var hasCode hasCode
	if errors.As(err, &hasCode) {
		return hasCode.code()
	}
	return 0
}
