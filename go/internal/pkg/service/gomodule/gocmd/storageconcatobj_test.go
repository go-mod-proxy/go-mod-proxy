package gocmd

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

type testResources struct {
	TmpDir string
	FD1    *os.File
	File1  string
	FD2    *os.File
	File2  string
	t      *testing.T
}

func (tt *testResources) dispose() {
	if tt.FD1 != nil {
		err := tt.FD1.Close()
		if err != nil {
			tt.t.Log(err)
			tt.t.Fail()
		}
	}
	if tt.FD2 != nil {
		err := tt.FD2.Close()
		if err != nil {
			tt.t.Log(err)
			tt.t.Fail()
		}
	}
	err := os.RemoveAll(tt.TmpDir)
	if err != nil {
		tt.t.Log(err)
		tt.t.Fail()
	}
}

func setup(t *testing.T) *testResources {
	tt := &testResources{
		t: t,
	}
	var err error
	tt.TmpDir, err = ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err != nil {
			tt.dispose()
		}
	}()
	tt.File1 = filepath.Join(tt.TmpDir, "file1")
	err = ioutil.WriteFile(tt.File1, []byte{1, 2, 3}, 0600)
	if err != nil {
		t.Log(err)
		t.Fail()
		return nil
	}
	tt.FD1, err = os.Open(tt.File1)
	if err != nil {
		t.Log(err)
		t.Fail()
		return nil
	}
	tt.File2 = filepath.Join(tt.TmpDir, "file2")
	err = ioutil.WriteFile(tt.File2, []byte{4, 5, 6}, 0600)
	if err != nil {
		t.Log(err)
		t.Fail()
		return nil
	}
	tt.FD2, err = os.Open(tt.File2)
	if err != nil {
		t.Log(err)
		t.Fail()
		return nil
	}
	return tt
}

func Test_ReaderForCreateConcatObj_Success1(t *testing.T) {
	tt := setup(t)
	defer tt.dispose()
	r := &readerForCreateConcatObj{
		prefix:  []byte{0},
		goModFD: tt.FD1,
		zipFD:   tt.FD2,
	}
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	if err != nil {
		t.Log(err)
		t.Fail()
		return
	}
	if !bytes.Equal(buf.Bytes(), []byte{0, 1, 2, 3, 4, 5, 6}) {
		t.Logf("%+v", buf.Bytes())
		t.Fail()
		return
	}
	_, err = r.Seek(0, io.SeekStart)
	if err != nil {
		t.Log(err)
		t.Fail()
		return
	}
	buf.Reset()
	_, err = io.Copy(&buf, r)
	if err != nil {
		t.Log(err)
		t.Fail()
		return
	}
	if !bytes.Equal(buf.Bytes(), []byte{0, 1, 2, 3, 4, 5, 6}) {
		t.Logf("%+v", buf.Bytes())
		t.Fail()
		return
	}
}

func Test_ReaderForCreateConcatObj_Success2(t *testing.T) {
	tt := setup(t)
	defer tt.dispose()
	r := &readerForCreateConcatObj{
		prefix:  []byte{1},
		goModFD: tt.FD1,
		zipFD:   tt.FD2,
	}
	var buf [1]byte
	_, err := r.Read(buf[:])
	if err != nil {
		t.Log(err)
		t.Fail()
		return
	}
	if !bytes.Equal(buf[:], []byte{1}) {
		t.Fail()
		return
	}
}
