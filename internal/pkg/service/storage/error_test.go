package storage

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_NewError(t *testing.T) {
	err := NewError(NotFound, "object not found")
	assert.Equal(t, &errorStruct{
		c: NotFound,
		s: "object not found",
	}, err)
}

func Test_NewErrorf(t *testing.T) {
	t.Run("C1", func(t *testing.T) {
		innerError := errors.New("404")
		outerError := NewErrorf(NotFound, "object not found: %w", innerError)
		if assert.Equal(t, &errorStruct{
			c:   NotFound,
			err: innerError,
			s:   "object not found: 404",
		}, outerError) {
			assert.Same(t, innerError, outerError.(*errorStruct).err)
		}
	})
	t.Run("C2", func(t *testing.T) {
		outerError := NewErrorf(PreconditionFailed, "object already exists: %v", "409")
		assert.Equal(t, &errorStruct{
			c: PreconditionFailed,
			s: "object already exists: 409",
		}, outerError)
	})
}

func Test_errorStruct(t *testing.T) {
	t.Run("Unwrap", func(t *testing.T) {
		t.Run("C1", func(t *testing.T) {
			innerError := errors.New("404")
			x := &errorStruct{err: innerError}
			assert.Same(t, innerError, x.Unwrap())
		})
		t.Run("C2", func(t *testing.T) {
			x := &errorStruct{}
			assert.Equal(t, nil, x.Unwrap())
		})
	})
	t.Run("Error", func(t *testing.T) {
		x := &errorStruct{s: "hi"}
		assert.Equal(t, "hi", x.Error())
	})
}

func Test_GetErrorCode(t *testing.T) {
	t.Run("C1", func(t *testing.T) {
		err := &errorStruct{c: PreconditionFailed}
		assert.Equal(t, PreconditionFailed, GetErrorCode(err))
	})
	t.Run("C2", func(t *testing.T) {
		innerError := &errorStruct{c: NotFound}
		outerError := fmt.Errorf("context: %w", innerError)
		assert.Equal(t, NotFound, GetErrorCode(outerError))
	})
	t.Run("C3", func(t *testing.T) {
		err := errors.New("hi")
		assert.Equal(t, ErrorCode(0), GetErrorCode(err))
	})
	t.Run("C4", func(t *testing.T) {
		assert.Equal(t, ErrorCode(0), GetErrorCode(nil))
	})
}

func Test_ErrorIsCode(t *testing.T) {
	err := &errorStruct{c: PreconditionFailed}
	assert.True(t, ErrorIsCode(err, PreconditionFailed))
	assert.False(t, ErrorIsCode(err, NotFound))
}
