package util

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_UnmarshalJSON(t *testing.T) {
	t.Run("FirstValueDecodeError1", func(t *testing.T) {
		x := &TestReadCloser{ReadData: []byte(`{"p`)}
		defer x.Close()
		var recv any
		err := UnmarshalJSON(x, &recv, false)
		assert.ErrorContains(t, err, "unexpected EOF")
	})
	t.Run("FirstValueDecodeError2", func(t *testing.T) {
		x := &TestReadCloser{ReadData: []byte(`{"p1":"","p2":""}`)}
		defer x.Close()
		var recv struct {
			P1 any `json:"p1"`
		}
		err := UnmarshalJSON(x, &recv, true)
		assert.ErrorContains(t, err, "unknown field")
	})
	t.Run("SecondValueDecodeError1", func(t *testing.T) {
		x := &TestReadCloser{ReadData: []byte(`{"p1":"v1"}{"p`)}
		defer x.Close()
		var recv struct {
			P1 any `json:"p1"`
		}
		err := UnmarshalJSON(x, &recv, false)
		assert.Equal(t, "v1", recv.P1)
		assert.ErrorContains(t, err, "unexpected EOF")
	})
	t.Run("MultipleValuesError", func(t *testing.T) {
		x := &TestReadCloser{ReadData: []byte(`"s1""s2"`)}
		defer x.Close()
		var recv any
		err := UnmarshalJSON(x, &recv, false)
		assert.Equal(t, "s1", recv)
		assert.ErrorContains(t, err, "unexpected seq of multiple JSON values")
	})
	t.Run("FirstValueReadErr", func(t *testing.T) {
		readErr := errors.New("network error")
		x := &TestReadCloser{ReadData: []byte(`{"p`), ReadErr: readErr}
		defer x.Close()
		var recv any
		err := UnmarshalJSON(x, &recv, false)
		assert.ErrorIs(t, err, readErr)
	})
	t.Run("SecondValueReadErr", func(t *testing.T) {
		readErr := errors.New("network error")
		x := &TestReadCloser{ReadData: []byte(`{"p1":"v1"}{"p`), ReadErr: readErr}
		defer x.Close()
		var recv struct {
			P1 any `json:"p1"`
		}
		err := UnmarshalJSON(x, &recv, false)
		assert.Equal(t, "v1", recv.P1)
		assert.ErrorIs(t, err, readErr)
	})
	t.Run("SuccessUnknownFields", func(t *testing.T) {
		x := &TestReadCloser{ReadData: []byte(`{"p1":"v1","p2":"v2"}`)}
		defer x.Close()
		var recv struct {
			P1 any `json:"p1"`
		}
		err := UnmarshalJSON(x, &recv, false)
		assert.NoError(t, err)
		assert.Equal(t, "v1", recv.P1)
	})
	t.Run("SuccessNoUnknownFields", func(t *testing.T) {
		x := &TestReadCloser{ReadData: []byte(`{"p1":"v1","p2":"v2"}`)}
		defer x.Close()
		var recv struct {
			P1 any `json:"p1"`
			P2 any `json:"p2"`
		}
		err := UnmarshalJSON(x, &recv, false)
		assert.NoError(t, err)
		assert.Equal(t, "v1", recv.P1)
		assert.Equal(t, "v2", recv.P2)
	})
}
