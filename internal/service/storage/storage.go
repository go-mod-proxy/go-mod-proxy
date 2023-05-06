package storage

import (
	"context"
	"io"
)

//

type Storage interface {
	// CreateObjectExclusively atomically creates an object named name with metadata metadata and data r if no object named name exists.
	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, PreconditionFailed)
	// is true if an object named name already exists.
	CreateObjectExclusively(ctx context.Context, name string, metadata ObjectMetadata, data io.ReadSeeker) error

	// DeleteObject deletes an object named name.
	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, NotFound)
	// is true if no object named name exists.
	DeleteObject(ctx context.Context, name string) error

	// GetObject returns the data of an object as a reader.
	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, NotFound)
	// is true if no object named name exists.
	GetObject(ctx context.Context, name string) (io.ReadCloser, error)

	// GetObjectMetadata returns the metadata of an object as a reader.
	// Returns an error e such that "github.com/go-mod-proxy/go-mod-proxy/internal/errors".ErrorIsCode(e, NotFound)
	// is true if no object named name exists.
	GetObjectMetadata(ctx context.Context, name string) (ObjectMetadata, error)

	// ListObjects returns a page of objects. A result *ObjectList o may have len(o.Names) < opts.MaxResults
	// even if there are more pages.
	ListObjects(ctx context.Context, opts ObjectListOptions) (*ObjectList, error)
}

type ObjectList struct {
	Names         []string
	NextPageToken string
}

type ObjectListOptions struct {
	// NamePrefix filters the object names to only those object names starting with NamePrefix.
	NamePrefix string

	// MaxResults is the maximum number of object names to return.
	// If MaxResults is 0 then the maximum number of object names to return is implemention-defined but non-zero.
	MaxResults int

	// PageToken ccan be set to o.NextPageToken where o is an *ObjectList returned from a call to ListObjects.
	PageToken string
}

type ObjectMetadata = map[string]string
