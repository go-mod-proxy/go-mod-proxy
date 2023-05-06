package util

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_TestReadCloser(t *testing.T) {
	t.Run("Close", func(t *testing.T) {
		t.Run("C1", func(t *testing.T) {
			errExp := errors.New("close error")
			x := TestReadCloser{
				CloseErr: errExp,
			}
			errActual := x.Close()
			assert.Equal(t, 1, x.CloseCount)
			assert.Same(t, errExp, errActual)
		})
		t.Run("C2", func(t *testing.T) {
			x := TestReadCloser{
				CloseCount: 1,
			}
			err := x.Close()
			assert.NoError(t, err)
			assert.Equal(t, 2, x.CloseCount)
		})
	})
	t.Run("Read", func(t *testing.T) {
		t.Run("C1", func(t *testing.T) {
			x := TestReadCloser{
				CloseCount: 1,
			}
			n, err := x.Read(nil)
			assert.Equal(t, 0, n)
			assert.Same(t, ErrReadAfterClose, err)
		})
		t.Run("C2", func(t *testing.T) {
			x := TestReadCloser{
				ReadData: []byte("abcdefg"),
			}
			p := make([]byte, 3)
			n, err := x.Read(p)
			assert.Equal(t, len(p), n)
			assert.NoError(t, err)
			assert.Equal(t, "abc", string(p))
		})
		t.Run("C3", func(t *testing.T) {
			const exp = "abcdefg"
			x := TestReadCloser{
				ReadData: []byte(exp),
			}
			p := make([]byte, len(exp))
			n, err := x.Read(p)
			assert.Equal(t, len(exp), n)
			assert.Same(t, io.EOF, err)
			assert.Equal(t, exp, string(p))
		})
	})
}
