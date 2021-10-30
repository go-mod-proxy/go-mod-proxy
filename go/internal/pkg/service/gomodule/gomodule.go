package gomodule

import (
	"context"
	"io"
	"time"

	"golang.org/x/mod/module"
)

// Info represents metadata of a particular module version. See Service.
type Info struct {
	Version string    // version string
	Time    time.Time // commit time
}

// Service is a strongly-typed interface for the Go module proxy protocol https://golang.org/cmd/go/#hdr-Module_proxy_protocol.
type Service interface {
	// Returns a non-nil error e such that ErrorIsCode(e, NotFound) is true if the specified module version does not exist.
	//	 Implementors can create an error e such that ErrorIsCode(e, NotFound) is true by calling NewErrorf(NotFound,, "...", ...).
	Info(ctx context.Context, moduleVersion *module.Version) (*Info, error)

	// Returns a non-nil error e such that ErrorIsCode(e, NotFound) is true if the specified module version does not exist.
	//	 Implementors can create an error e such that ErrorIsCode(e, NotFound) is true by calling NewErrorf(NotFound,, "...", ...).
	Latest(ctx context.Context, modulePath string) (*Info, error)

	// List returns an io.ReadCloser who's byte stream is the concatenation of version+"\n" for each version of the specified module.
	List(ctx context.Context, modulePath string) (io.ReadCloser, error)

	// Zip returns an an io.ReadCloser who's byte stream is a zip archive containing all relevant files of the specified module version.s
	// Returns a non-nil error e such that ErrorIsCode(e, NotFound) is true if the specified module version does not exist.
	//	 Implementors can create an error e such that ErrorIsCode(e, NotFound) is true by calling NewErrorf(NotFound,, "...", ...).
	Zip(ctx context.Context, moduleVersion *module.Version) (io.ReadCloser, error)

	// GoMod returns an io.ReadCloser who's byte stream is the go.mod file (UTF-8 encoded) of the specified module version.
	// Returns a non-nil error e such that ErrorIsCode(e, NotFound) is true if the specified module version does not exist.
	//	 Implementors can create an error e such that ErrorIsCode(e, NotFound) is true by calling NewErrorf(NotFound,, "...", ...).
	GoMod(ctx context.Context, moduleVersion *module.Version) (io.ReadCloser, error)
}
