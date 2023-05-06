package modproxyclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	module "golang.org/x/mod/module"
)

var (
	ErrNotFound = errors.New("not found")
)

func doRequestCommon(ctx context.Context, baseURL string, client *http.Client, modulePath, urlSuffix string) (*http.Response, error) {
	url, err := getURL(baseURL, modulePath, urlSuffix)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("server gave unexpected %d-response to request %s %s: %s",
			resp.StatusCode,
			resp.Request.Method,
			resp.Request.URL.String(),
			string(respBodyBytes))
	}
	return resp, nil
}

func getURL(baseURL, modulePath, suffix string) (string, error) {
	var sb strings.Builder
	sb.Grow(len(baseURL) + len(modulePath) + len(suffix))
	sb.WriteString(baseURL)
	modulePathEscaped, err := module.EscapePath(modulePath)
	if err != nil {
		return "", fmt.Errorf("modulePath is invalid: %v", err)
	}
	r := modulePathEscaped
	for {
		i := strings.IndexByte(r, '/')
		var rComponent string
		if i < 0 {
			rComponent = r
		} else {
			rComponent = r[:i]
		}
		sb.WriteString(url.PathEscape(rComponent))
		if i < 0 {
			break
		}
		r = r[i+1:]
		sb.WriteByte('/')
	}
	sb.WriteString(suffix)
	return sb.String(), nil
}
