package gocmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
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

func httpList(ctx context.Context, baseURL string, client *http.Client, modulePath string) ([]string, error) {
	modulePathEscaped, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, fmt.Errorf("modulePath is invalid: %v", err)
	}
	url := baseURL + modulePathEscaped + "/@v/list"
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
	bytesRemainding, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body of %d-response to %s %s: %v", resp.StatusCode,
			req.Method, url, err)
	}
	var versions []string
	for len(bytesRemainding) > 0 {
		i := bytes.IndexByte(bytesRemainding, '\n')
		if i < 0 {
			return nil, fmt.Errorf("body of response to %s %s unexpectedly is not empty and does not end with a line-feed",
				req.Method, url)
		}
		version := string(bytesRemainding[:i])
		if version == "" {
			return nil, fmt.Errorf("body of response to %s %s unexpectedly contains an empty line", req.Method, url)
		}
		versions = append(versions, version)
		bytesRemainding = bytesRemainding[i+1:]
	}
	return versions, nil
}
