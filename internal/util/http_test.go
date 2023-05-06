package util

import (
	"errors"
	"net/http"
	urlpkg "net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ReadJSON200Response(t *testing.T) {
	makeResp := func(t *testing.T, statusCode int) (*http.Response, *TestReadCloser) {
		t.Helper()
		req := &http.Request{
			Method: http.MethodPut,
		}
		var err error
		req.URL, err = urlpkg.Parse("https://github.com/test")
		if err != nil {
			t.Fatal(err)
		}
		body := &TestReadCloser{}
		resp := &http.Response{
			Body:       body,
			Request:    req,
			StatusCode: statusCode,
		}
		return resp, body
	}
	t.Run("Non200NoReadErr", func(t *testing.T) {
		resp, respBody := makeResp(t, 400)
		respBody.ReadData = []byte("something went wrong")
		err := ReadJSON200Response(resp, nil, false)
		assert.Equal(t, 1, respBody.CloseCount)
		if assert.Error(t, err) {
			assert.Equal(t, "server gave unexpected 400-response for request PUT https://github.com/test: something went wrong", err.Error())
		}
	})
	t.Run("Non200ReadErr", func(t *testing.T) {
		resp, respBody := makeResp(t, 400)
		respBody.ReadData = []byte("something we")
		respBody.ReadErr = errors.New("network error")
		err := ReadJSON200Response(resp, nil, false)
		assert.Equal(t, 1, respBody.CloseCount)
		if assert.Error(t, err) {
			assert.Equal(t, "server gave unexpected 400-response for request PUT https://github.com/test: something we", err.Error())
		}
	})
	t.Run("200ReadErr", func(t *testing.T) {
		resp, respBody := makeResp(t, 200)
		respBody.ReadData = []byte("{}")
		readErr := errors.New("network error")
		respBody.ReadErr = readErr
		err := ReadJSON200Response(resp, nil, false)
		assert.Equal(t, 1, respBody.CloseCount)
		if assert.Error(t, err) {
			assert.ErrorIs(t, err, readErr)
			assert.Equal(t, "error reading body of success response for request PUT https://github.com/test: network error", err.Error())
		}
	})
	t.Run("200UnmarshalErr", func(t *testing.T) {
		resp, respBody := makeResp(t, 200)
		respBody.ReadData = []byte("{")
		var respBodyInterf any
		err := ReadJSON200Response(resp, &respBodyInterf, false)
		assert.Equal(t, 1, respBody.CloseCount)
		assert.ErrorContains(t, err, "error unmarshalling body of success response for request PUT https://github.com/test: ")
	})
	t.Run("200Success", func(t *testing.T) {
		resp, respBody := makeResp(t, 200)
		respBody.ReadData = []byte(`{"hello":"world"}`)
		var respBodyInterf any
		err := ReadJSON200Response(resp, &respBodyInterf, false)
		assert.Equal(t, 1, respBody.CloseCount)
		if assert.NoError(t, err) {
			assert.Equal(t, map[string]interface{}{
				"hello": "world",
			}, respBodyInterf)
		}
	})
}
