package gomodule

import (
	"context"
	"io"
	"time"

	"golang.org/x/mod/module"
)

// Info represents metadata of a particular module version. See Service.
type Info struct {
	// As per https://github.com/golang/go/blob/ebf8e26d03d3c01bf1611b1189e0af64c3698557/src/cmd/go/internal/modfetch/repo.go#L77.

	Origin any
	// Origin uses type any because we disallow unknown fields (so we know when new fields need to be added),
	// and the struct is internal:
	// https://github.com/golang/go/blob/ebf8e26d03d3c01bf1611b1189e0af64c3698557/src/cmd/go/internal/modfetch/codehost/codehost.go#L92

	Version string    // version string
	Time    time.Time // commit time
}

// Service is a strongly-typed interface for the Go module proxy protocol https://golang.org/cmd/go/#hdr-Module_proxy_protocol.
type Service interface {

	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, NotFound)
	// is true if the specified module version does not exist.
	Info(ctx context.Context, moduleVersion *module.Version) (*Info, error)

	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, NotFound)
	// is true if the specified module version does not exist.
	Latest(ctx context.Context, modulePath string) (*Info, error)

	// List returns an io.ReadCloser who's byte stream is the concatenation of version+"\n" for each version of the specified module.
	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, NotFound)
	// is true if the specified module does not exist.
	List(ctx context.Context, modulePath string) (io.ReadCloser, error)

	// Zip returns an an io.ReadCloser who's byte stream is a zip archive containing all relevant files of the specified module version.s
	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, NotFound)
	// is true if the specified module version does not exist.
	Zip(ctx context.Context, moduleVersion *module.Version) (io.ReadCloser, error)

	// GoMod returns an io.ReadCloser who's byte stream is the go.mod file of the specified module version.
	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, NotFound)
	// is true if the specified module version does not exist.
	GoMod(ctx context.Context, moduleVersion *module.Version) (io.ReadCloser, error)
}
