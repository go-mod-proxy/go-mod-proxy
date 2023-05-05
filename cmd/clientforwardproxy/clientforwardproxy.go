package clientforwardproxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/hashicorp/go-cleanhttp"
	jasperurl "github.com/jbrekelmans/go-lib/url"
)

// CLI is a type reflected by "github.com/alecthomas/kong" that configures the CLI command for the client forward proxy.
//
//nolint:govet // linter does not like the syntax required by the kong package
type CLI struct {
	Password  string `required help:"Password component of credentials to access server"`
	Port      int    `required help:"Port to listen on"`
	ServerURL string `required help:"URL of the server"`
	User      string `required help:"Username component of credentials to access server"`
}

type app struct {
	ctx          context.Context
	httpClient   *http.Client
	password     string
	reverseProxy *httputil.ReverseProxy
	serverURL    *url.URL
	user         string
}

func (a *app) reverseProxyDirector(req *http.Request) {
	req.URL.Scheme = a.serverURL.Scheme
	req.URL.Host = a.serverURL.Host
	req.URL.Path = a.serverURL.Path + "/" + strings.TrimPrefix(req.URL.Path, "/")
	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
	if _, ok := req.Header["Authorization"]; !ok {
		req.SetBasicAuth(a.user, a.password)
	}
}

func Run(ctx context.Context, opts *CLI) error {
	if ctx == nil {
		return fmt.Errorf("ctx must not be nil")
	}
	var err error
	a := &app{
		ctx: ctx,
	}
	a.serverURL, err = jasperurl.ValidateURL(opts.ServerURL, jasperurl.ValidateURLOptions{
		Abs:                                      jasperurl.NewBool(true),
		AllowedSchemes:                           []string{"https"},
		StripFragment:                            true,
		StripQuery:                               true,
		StripPathTrailingSlashes:                 true,
		StripPathTrailingSlashesNoPercentEncoded: true,
		User:                                     new(bool),
	})
	if err != nil {
		return fmt.Errorf("server URL is invalid: %w", err)
	}
	if i := strings.IndexByte(opts.User, ':'); i >= 0 {
		return fmt.Errorf("user contains illegal character ':'")
	}
	a.httpClient = cleanhttp.DefaultPooledClient()
	a.reverseProxy = httputil.NewSingleHostReverseProxy(a.serverURL)
	a.reverseProxy.Transport = a.httpClient.Transport
	a.reverseProxy.Director = a.reverseProxyDirector
	err = http.ListenAndServe(fmt.Sprintf(":%d", opts.Port), a.reverseProxy)
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
