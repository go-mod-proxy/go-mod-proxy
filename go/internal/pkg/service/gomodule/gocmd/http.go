package gocmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	module "golang.org/x/mod/module"

	gomoduleservice "github.com/go-mod-proxy/go/internal/pkg/service/gomodule"
	"github.com/go-mod-proxy/go/internal/pkg/util"
)

var errHTTPNotFound = errors.New("not found")

func httpLatest(ctx context.Context, baseURL string, client *http.Client, modulePath string) (*gomoduleservice.Info, error) {
	modulePathEscaped, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, fmt.Errorf("modulePath is invalid: %v", err)
	}
	url := baseURL + modulePathEscaped + "/@latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			return nil, errHTTPNotFound
		}
		return nil, fmt.Errorf("server gave unexpected %d-response to %s %s", resp.StatusCode, req.Method, url)
	}
	x := &gomoduleservice.Info{}
	if err := util.UnmarshalJSON(resp.Body, x, false); err != nil {
		return nil, fmt.Errorf("error unmarshalling body of %d-response to %s %s: %v", resp.StatusCode,
			req.Method, url, err)
	}
	return x, nil
}
