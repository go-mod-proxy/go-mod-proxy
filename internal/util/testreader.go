package util

import (
	"errors"
	"io"
)

// ErrReadAfterClose is a sentinel error returned by (*TestReadCloser).Read.
var ErrReadAfterClose = errors.New("cannot Read from closed *TestReadCloser")

type TestReadCloser struct {
	CloseCount int
	CloseErr   error

	// ReadData is data returned by Read.
	ReadData []byte
	// ReadErr is an error to return from Read after ReadData is consumed.
	ReadErr error
}

var _ io.ReadCloser = (*TestReadCloser)(nil)

func (t *TestReadCloser) Close() error {
	t.CloseCount++
	return t.CloseErr
}

func (t *TestReadCloser) Read(p []byte) (n int, err error) {
	if t.CloseCount > 0 {
		err = ErrReadAfterClose
		return
	}
	n = copy(p, t.ReadData)
	t.ReadData = t.ReadData[n:]
	if len(t.ReadData) == 0 {
		err = t.ReadErr
		if err == nil {
			err = io.EOF
		}
	}
	return
}
