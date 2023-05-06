package util

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func ReadJSON200Response(resp *http.Response, respBody any, disallowUnknownFields bool) error {
	defer resp.Body.Close()
	respBodyBytes, err := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("server gave unexpected %d-response for request %s %s: %s",
			resp.StatusCode,
			resp.Request.Method,
			resp.Request.URL.String(),
			string(respBodyBytes))
	}
	if err != nil {
		return fmt.Errorf("error reading body of success response for request %s %s: %w",
			resp.Request.Method,
			resp.Request.URL.String(),
			err)
	}
	err = UnmarshalJSON(bytes.NewReader(respBodyBytes), respBody, disallowUnknownFields)
	if err != nil {
		return fmt.Errorf("error unmarshalling body of success response for request %s %s: %w",
			resp.Request.Method,
			resp.Request.URL.String(),
			err)
	}
	return nil
}
