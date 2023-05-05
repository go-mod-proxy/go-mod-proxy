package credentialhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/go-cleanhttp"
)

func doRequest(ctx context.Context, port int, reqBody, respBody interface{}) error {
	transport := cleanhttp.DefaultTransport()
	transport.Proxy = nil
	httpClient := &http.Client{
		Transport: transport,
	}
	method := http.MethodPost
	url := fmt.Sprintf("http://127.0.0.1:%d/git", port)
	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("error marshalling body of request %s %s: %w", method, url, err)
	}
	reqBodyReader := bytes.NewReader(reqBodyBytes)
	req, err := http.NewRequestWithContext(ctx, method, url, reqBodyReader)
	if err != nil {
		return fmt.Errorf("error creating request %s %s: %w", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBodyBytes, err2 := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("response of request %s %s has unexpected status %d, body: %s", method, url, resp.StatusCode, string(respBodyBytes))
	}
	if err2 != nil {
		return fmt.Errorf("error reading body of success response of request %s %s: %w", method, url, err)
	}
	err = json.Unmarshal(respBodyBytes, respBody)
	if err != nil {
		return fmt.Errorf("error unmarshalling body of success response of request %s %s: %w", method, url, err)
	}
	return nil
}
