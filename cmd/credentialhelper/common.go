package credentialhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/go-cleanhttp"

	"github.com/go-mod-proxy/go-mod-proxy/internal/util"
)

func doRequest(ctx context.Context, port int, reqBody, respBody any) error {
	transport := cleanhttp.DefaultTransport()
	transport.Proxy = nil
	httpClient := &http.Client{
		Transport: transport,
	}
	const method = http.MethodPost
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
	return util.ReadJSON200Response(resp, respBody, false)
}
