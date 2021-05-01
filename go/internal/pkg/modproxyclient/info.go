package modproxyclient

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"golang.org/x/mod/module"
)

func Info(ctx context.Context, baseURL string, client *http.Client, moduleVersion *module.Version) ([]string, error) {
	versionEscaped := url.PathEscape(moduleVersion.Version)

	resp, err := doRequestCommon(ctx, baseURL, client, moduleVersion.Path, "/@v/"+versionEscaped+".info")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bytesRemaining, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body of %d-response to %s %s: %v", resp.StatusCode,
			resp.Request.Method, resp.Request.URL.String(), err)
	}
	var versions []string
	for len(bytesRemaining) > 0 {
		i := bytes.IndexByte(bytesRemaining, '\n')
		if i < 0 {
			return nil, fmt.Errorf("body of response to %s %s unexpectedly is not empty and does not end with a line-feed",
				resp.Request.Method, resp.Request.URL.String())
		}
		version := string(bytesRemaining[:i])
		if version == "" {
			return nil, fmt.Errorf("body of response to %s %s unexpectedly contains an empty line",
				resp.Request.Method, resp.Request.URL.String())
		}
		versions = append(versions, version)
		bytesRemaining = bytesRemaining[i+1:]
	}
	return versions, nil
}
