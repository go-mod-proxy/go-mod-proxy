package gomodule

import (
	"errors"
	"fmt"
)

type ErrorCode int

const (
	NotFound ErrorCode = iota

	// ParentProxyGoogleVPCError indicates an error occured downloading a module from the parent proxy and the error is known to indicate
	// that the parent proxy returned a HTTP 302 redirect response to a signed URL of a Google Cloud Storage (GCS) object, but the Google Cloud
	// Storage object could not be downloaded because Google Virtual Private Cloud (VPC) Service Controls blocked it.
	// This error code exists to work around a misconfiguration in parent proxies that are in the same VPC as this module proxy, but do
	// not proxy GCS reqquests to outside the VPC.
	ParentProxyGoogleVPCError
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
