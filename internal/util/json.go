package util

import (
	"encoding/json"
	"fmt"
	"io"
)

func UnmarshalJSON(reader io.Reader, v interface{}, disallowUnknownFields bool) error {
	decoder := json.NewDecoder(reader)
	if disallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var vNext interface{}
	if err := decoder.Decode(&vNext); err == nil {
		return fmt.Errorf("unexpectedly got a sequence of multiple JSON values when a single JSON value is expected")
	} else if err != io.EOF {
		return err
	}
	return nil
}
