package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

var regexpSimpleName = regexp.MustCompile("^[a-zA-Z0-9]+$")

type errorBag struct {
	errors []string
	mu     sync.Mutex
}

func newErrorBag() *errorBag {
	return &errorBag{}
}

func (e *errorBag) AddError(path, error string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if path == "" {
		path = "."
	}
	e.errors = append(e.errors, fmt.Sprintf("error at %s: %s", path, error))
}

func (e *errorBag) ErrorCount() int {
	return len(e.errors)
}

func (e *errorBag) Err() error {
	if len(e.errors) > 0 {
		return errors.New(fmt.Sprintf("got %d error(s):\n  - ", len(e.errors)) + strings.Join(e.errors, "\n  - "))
	}
	return nil
}

type validateValueContext struct {
	errorBag *errorBag
	path     string
}

func (v *validateValueContext) AddError(error string) {
	v.errorBag.AddError(v.path, error)
}

func (v *validateValueContext) AddErrorf(format string, args ...interface{}) {
	v.AddError(fmt.Sprintf(format, args...))
}

func (v *validateValueContext) AddRequiredError() {
	v.AddError("value must be set (to a non-null value)")
}

func (v *validateValueContext) Child(x interface{}) *validateValueContext {
	switch y := x.(type) {
	case byte, int, int8, int16, int32, int64, uint, uint16, uint32, uint64:
		return &validateValueContext{
			errorBag: v.errorBag,
			path:     fmt.Sprintf("%s[%d]", v.path, y),
		}
	case string:
		var childPath string
		if regexpSimpleName.MatchString(y) {
			childPath = fmt.Sprintf("%s.%s", v.path, y)
		} else {
			childPath = fmt.Sprintf("%s[%#v]", v.path, y)
		}
		return &validateValueContext{
			errorBag: v.errorBag,
			path:     childPath,
		}
	default:
		panic(fmt.Sprintf("x has unexpected type %T", x))
	}
}

func (v *validateValueContext) ErrorCount() int {
	return v.errorBag.ErrorCount()
}

func (v *validateValueContext) RequiredString(s string) bool {
	if s == "" {
		v.AddError("value must be set (to a non-empty string)")
		return false
	}
	return true
}
