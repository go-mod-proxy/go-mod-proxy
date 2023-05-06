package modproxyclient

import (
	"context"
	"fmt"
	"net/http"

	gomoduleservice "github.com/go-mod-proxy/go-mod-proxy/internal/service/gomodule"
	"github.com/go-mod-proxy/go-mod-proxy/internal/util"
)

func Latest(ctx context.Context, baseURL string, client *http.Client, modulePath string) (*gomoduleservice.Info, error) {
	resp, err := doRequestCommon(ctx, baseURL, client, modulePath, "/@latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	x := &gomoduleservice.Info{}
	if err := util.UnmarshalJSON(resp.Body, x, false); err != nil {
		return nil, fmt.Errorf("error unmarshalling body of %d-response to %s %s: %v", resp.StatusCode,
			resp.Request.Method, resp.Request.URL.String(), err)
	}
	return x, nil
}
