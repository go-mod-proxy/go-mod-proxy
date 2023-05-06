package util

import (
	"io"
)

type ConcatReader struct {
	close  func() error
	prefix []byte
	pos    int
	r      io.Reader
	suffix []byte
}

var _ io.Reader = (*ConcatReader)(nil)

func NewConcatReader(prefix []byte, r io.Reader, suffix []byte, close func() error) *ConcatReader {
	return &ConcatReader{
		close:  close,
		prefix: prefix,
		r:      r,
		suffix: suffix,
	}
}

func (c *ConcatReader) Close() error {
	if c.close != nil {
		return c.close()
	}
	return nil
}

func (c *ConcatReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return
	}
	if c.pos < len(c.prefix) {
		n = copy(p, c.prefix[c.pos:])
		c.pos += n
		if n == len(p) {
			return
		}
	}
	if c.pos >= 0 {
		if c.r != nil {
			n2, err2 := c.r.Read(p[n:])
			n += n2
			if err2 == nil {
				return
			} else if err2 != io.EOF {
				err = err2
				return
			}
		}
		c.pos = -1
	}
	n2 := copy(p[n:], c.suffix[-c.pos-1:])
	n += n2
	c.pos -= n2
	if -c.pos-1 == len(c.suffix) {
		err = io.EOF
	}
	return
}
