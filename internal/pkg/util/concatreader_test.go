package util

import (
	"bytes"
	"io"
	"testing"
)

func Test_ConcatReader_Read(t *testing.T) {
	c := NewConcatReader([]byte{1}, bytes.NewReader([]byte{2}), []byte{3}, nil)
	var buf bytes.Buffer
	n, err := io.Copy(&buf, c)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatal(n)
	}
	if !bytes.Equal(buf.Bytes(), []byte{1, 2, 3}) {
		t.Logf("%+v", buf.Bytes())
		t.Fail()
	}
}
