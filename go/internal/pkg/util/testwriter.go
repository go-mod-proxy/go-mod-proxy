package util

import (
	"bytes"
)

type TestWriter struct {
	Buffer bytes.Buffer
	err    error
	// return err from Write(p) after errAfter bytes have been written
	errAfter int
}

func NewTestWriter(err error, errAfter int) (*TestWriter, error) {
	t := &TestWriter{
		err:      err,
		errAfter: errAfter,
	}
	return t, nil
}

func (t *TestWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return
	}
	if t.errAfter > 0 {
		if len(p) > t.errAfter {
			p = p[:t.errAfter]
		}
		n, err = t.Buffer.Write(p)
		t.errAfter -= n
		if err != nil {
			return
		}
	}
	if t.errAfter == 0 {
		err = t.err
	}
	return
}
