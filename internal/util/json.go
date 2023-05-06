package util

import (
	"encoding/json"
	"fmt"
	"io"
)

func UnmarshalJSON(reader io.Reader, v any, disallowUnknownFields bool) error {
	decoder := json.NewDecoder(reader)
	if disallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var vNext any
	if err := decoder.Decode(&vNext); err == nil {
		return fmt.Errorf("unexpected seq of multiple JSON values")
	} else if err != io.EOF {
		return err
	}
	return nil
}
