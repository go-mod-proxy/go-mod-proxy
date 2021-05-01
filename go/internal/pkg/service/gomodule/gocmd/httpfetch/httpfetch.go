package httpfetch

import (
	"context"
	"net/http"
	
)

func Fetch(ctx context.Context, opts FetchOptions) error {
	fetcher, err := newFetcher(opts)
	if err != nil {
		return err
	}
	return fetcher.Run(ctx)
}

type fetcher struct {
	httpClient *http.Client
	modulePath string
	version    string
}

func newFetcher() (*fetcher, error) {
	return &fetcher{}, nil
}

func (f *fetcher) Run(ctx context.Context) error {

	return nil
}

type FetchOptions struct {
	ModulePath string
	Version    string
}
