package git

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/util"
)

func Test_WriteConfigSectionName_SuccessDot(t *testing.T) {
	var buf bytes.Buffer
	err := writeConfigSectionName(&buf, "asdf.asdf")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), []byte(`[asdf "asdf"]`+"\n")) {
		t.Fail()
	}
}
func Test_WriteConfigSectionName_SuccessNoDot(t *testing.T) {
	var buf bytes.Buffer
	err := writeConfigSectionName(&buf, "asdf")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), []byte(`[asdf]`+"\n")) {
		t.Fail()
	}
}

func Test_WriteConfigSectionName_ErrorZeroByte(t *testing.T) {
	var buf bytes.Buffer
	err := writeConfigSectionName(&buf, "\x00")
	if err == nil || !strings.Contains(err.Error(), "zero byte") {
		t.Fatal(err)
	}
}

func Test_WriteConfigSectionName_ErrorDotWriterError1(t *testing.T) {
	errExpected := errors.New(`1`)
	w, err := util.NewTestWriter(errExpected, 0)
	if err != nil {
		t.Fatal(err)
	}
	errActual := writeConfigSectionName(w, "asdf.asdf")
	if errActual != errExpected {
		t.Fatal(err)
	}
}

func Test_WriteConfigSectionName_ErrorDotWriterError2(t *testing.T) {
	errExpected := errors.New(`2`)
	w, err := util.NewTestWriter(errExpected, 8)
	if err != nil {
		t.Fatal(err)
	}
	errActual := writeConfigSectionName(w, "asdf.a\"")
	if errActual != errExpected {
		t.Fatal(err)
	}
}

func Test_WriteConfigSectionName_ErrorDotWriterError3(t *testing.T) {
	errExpected := errors.New(`3`)
	w, err := util.NewTestWriter(errExpected, 8)
	if err != nil {
		t.Fatal(err)
	}
	errActual := writeConfigSectionName(w, "asdf.\"")
	if errActual != errExpected {
		t.Fatal(err)
	}
}

func Test_WriteConfigSectionName_ErrorDotWriterError4(t *testing.T) {
	errExpected := errors.New(`4`)
	w, err := util.NewTestWriter(errExpected, 12)
	if err != nil {
		t.Fatal(err)
	}
	errActual := writeConfigSectionName(w, "asdf.as\"f")
	if errActual != errExpected {
		t.Fatal(err)
	}
}

func Test_WriteConfigKeyValuePair_SuccessNotQuoted(t *testing.T) {
	var buf bytes.Buffer
	err := writeConfigKeyValuePair(&buf, "k", "v\n\t\"\\")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), []byte("\tk = v\\n\\t\\\"\\\\\n")) {
		t.Fail()
	}
}

func Test_WriteConfigKeyValuePair_SuccessQuoted(t *testing.T) {
	var buf bytes.Buffer
	err := writeConfigKeyValuePair(&buf, "k", " v ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), []byte("\tk = \" v \"\n")) {
		t.Fail()
	}
}

func Test_WriteConfigKeyValuePair_ErrorKeyZeroByte(t *testing.T) {
	var buf bytes.Buffer
	err := writeConfigKeyValuePair(&buf, "\x00", "v")
	if err == nil || err.Error() != "key contains illegal zero byte" {
		t.Fatal(err)
	}
}

func Test_WriteConfigKeyValuePair_ErrorValueZeroByte(t *testing.T) {
	var buf bytes.Buffer
	err := writeConfigKeyValuePair(&buf, "k", "\x00")
	if err == nil || err.Error() != "value contains illegal zero byte" {
		t.Fatal(err)
	}
}

func Test_WriteConfigKeyValuePair_ErrorWriterError1(t *testing.T) {
	errExpected := errors.New(`1`)
	w, err := util.NewTestWriter(errExpected, 0)
	if err != nil {
		t.Fatal(err)
	}
	errActual := writeConfigKeyValuePair(w, "k", "v")
	if errActual != errExpected {
		t.Fatal(err)
	}
}

func Test_WriteConfigKeyValuePair_ErrorWriterError2(t *testing.T) {
	errExpected := errors.New(`1`)
	w, err := util.NewTestWriter(errExpected, 6)
	if err != nil {
		t.Fatal(err)
	}
	errActual := writeConfigKeyValuePair(w, "k", "\n")
	if errActual != errExpected {
		t.Fatal(err)
	}
}

func Test_WriteConfigKeyValuePair_ErrorWriterError3(t *testing.T) {
	errExpected := errors.New(`1`)
	w, err := util.NewTestWriter(errExpected, 6)
	if err != nil {
		t.Fatal(err)
	}
	errActual := writeConfigKeyValuePair(w, "k", "\t")
	if errActual != errExpected {
		t.Fatal(err)
	}
}
func Test_WriteConfigKeyValuePair_ErrorWriterError4(t *testing.T) {
	errExpected := errors.New(`1`)
	w, err := util.NewTestWriter(errExpected, 6)
	if err != nil {
		t.Fatal(err)
	}
	errActual := writeConfigKeyValuePair(w, "k", "\"")
	if errActual != errExpected {
		t.Fatal(err)
	}
}
