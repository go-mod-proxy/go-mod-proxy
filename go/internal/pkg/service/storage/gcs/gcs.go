package gcs

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	gcs "cloud.google.com/go/storage"
	gax "github.com/googleapis/gax-go/v2"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/googleapi"

	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/service/storage"
	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/util"
)

const (
	uint64Max = ^uint64(0)
	int64Max  = int64(uint64Max >> 1)

	uploadBaseURL = "https://storage.googleapis.com/upload/storage/v1"
	baseURL       = "https://storage.googleapis.com/storage/v1"
)

type StorageOptions struct {
	Bucket    string
	GCSClient *gcs.Client
	// HTTPClient is asumed to add Authorization headers on requests
	HTTPClient *http.Client
}

type Storage struct {
	bucket          string
	gcsClient       *gcs.Client
	gcsClientBucket *gcs.BucketHandle
	httpClient      *http.Client
	objectListURL   string
	uploadURL       string
}

var _ storage.Storage = (*Storage)(nil)

func NewStorage(opts StorageOptions) (*Storage, error) {
	if opts.Bucket == "" {
		return nil, fmt.Errorf("opts.Bucket must not be empty")
	}
	if opts.GCSClient == nil {
		return nil, fmt.Errorf("opts.GCSClient must not be nil")
	}
	if opts.HTTPClient == nil {
		return nil, fmt.Errorf("opts.HTTPClient must not be nil")
	}
	s := &Storage{
		bucket:          opts.Bucket,
		gcsClient:       opts.GCSClient,
		gcsClientBucket: opts.GCSClient.Bucket(opts.Bucket),
		httpClient:      opts.HTTPClient,
		objectListURL:   baseURL + "/b/" + url.PathEscape(opts.Bucket) + "/o",
		uploadURL:       uploadBaseURL + "/b/" + url.PathEscape(opts.Bucket) + "/o",
	}
	return s, nil
}

func (s *Storage) CreateObjectExclusively(ctx context.Context,
	name string, objectMetadata storage.ObjectMetadata,
	data io.ReadSeeker) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	// This can also be implemented using s.gcsClientBucket.Object(name).
	//		If(gcs.Conditions{DoesNotExist:true}).
	//		NewWriter(ctx)
	// but this implementation does a single multipart/related HTTP request rather
	// than 2 or more requests with s.gcsClientBucket approach (assuming non-empty body), and
	// sets the fields parameter.
	// A disadvantage is that there's more code in our case.
	method := http.MethodPost
	urlQuery := url.Values{}
	urlQuery.Set("ifGenerationMatch", "0")

	// Set fields query parameter to reduce response body size.
	// We don't care about any response fields but in case setting fields to an empty string
	// is equivalent to not setting it, we instead specify to return a single field ("generation").
	urlQuery.Set("fields", "generation")

	urlQuery.Set("prettyPrint", "false")
	urlQuery.Set("uploadType", "multipart")
	url := s.uploadURL + "?" + urlQuery.Encode()

	dataMD5H := md5.New()
	dataLength, err := io.Copy(dataMD5H, data)
	if err != nil {
		return err
	}
	dataMD5 := dataMD5H.Sum(nil)
	dataMD5Base64 := base64.StdEncoding.EncodeToString(dataMD5)
	var respBody io.ReadCloser
	defer func() {
		if respBody != nil {
			err2 := respBody.Close()
			if err2 != nil {
				log.Errorf("error closing body of response to %s %s: %v", method, url, err2)
			}
		}
	}()
	// Inspired by https://github.com/googleapis/google-cloud-go/blob/67b19f0bd698c1df21addff89060b4356816a4d3/storage/invoke.go#L26
	var backoff gax.Backoff
	for {
		_, err = data.Seek(0, io.SeekStart)
		if err != nil {
			return err
		}

		var boundaryArr [32]byte
		_, err = rand.Read(boundaryArr[:])
		if err != nil {
			return err
		}
		boundaryStr := fmt.Sprintf("%x", string(boundaryArr[:]))

		var reqBodyPrefix bytes.Buffer
		fmt.Fprintf(&reqBodyPrefix, "--%s\r\nContent-Type: application/json; charset=UTF-8\r\n\r\n", boundaryStr)
		if err := json.NewEncoder(&reqBodyPrefix).Encode(struct {
			MD5Hash  string            `json:"md5Hash"`
			Metadata map[string]string `json:"metadata"`
			Name     string            `json:"name"`
		}{
			MD5Hash:  dataMD5Base64,
			Metadata: objectMetadata,
			Name:     name,
		}); err != nil {
			panic(err)
		}
		fmt.Fprintf(&reqBodyPrefix, "\r\n--%s\r\n\r\n", boundaryStr)

		reqBodySuffix := []byte(fmt.Sprintf("\r\n--%s\r\n", boundaryStr))
		contentLength := int64(reqBodyPrefix.Len())
		if int64Max-contentLength < dataLength {
			return fmt.Errorf("integer overflow")
		}
		contentLength += dataLength
		if int64Max-contentLength < int64(len(reqBodySuffix)) {
			return fmt.Errorf("integer overflow")
		}
		contentLength += int64(len(reqBodySuffix))

		req, err := http.NewRequestWithContext(ctx, method, url, util.NewConcatReader(reqBodyPrefix.Bytes(), data, reqBodySuffix, nil))
		if err != nil {
			return fmt.Errorf("error creating request %s %s: %w", method, url, err)
		}
		req.Header.Set("Content-Length", strconv.FormatInt(contentLength, 10))
		req.Header.Set("Content-Type", fmt.Sprintf("multipart/related; boundary=%s", boundaryStr))
		resp, err := s.httpClient.Do(req)
		if err != nil {
			if !shouldRetryDoRequest(err) {
				return fmt.Errorf("error doing request %s %s: %w", method, url, err)
			}
			log.Errorf("retrying because got intermittent error doing request %s %s: %v", method, url, err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff.Pause()):
			}
			continue
		}
		respBody = resp.Body
		if resp.StatusCode == http.StatusOK {
			_, err := io.Copy(io.Discard, respBody)
			if err != nil {
				log.Errorf("error reading body of %d-response to %s %s: %v", resp.StatusCode, method, url, err)
			}
			break
		}
		respBodyBytes, err := io.ReadAll(respBody)
		if err != nil {
			log.Errorf("error reading body of %d-response to %s %s: %v", resp.StatusCode, method, url, err)
		}
		err = respBody.Close()
		respBody = nil
		if err != nil {
			log.Errorf("error closing body of %d-response to %s %s: %v", resp.StatusCode, method, url, err)
		}
		if resp.StatusCode == http.StatusTooManyRequests || (500 <= resp.StatusCode && resp.StatusCode <= 599) {
			log.Errorf("retrying because got intermittent %d-response to %s %s: %s", resp.StatusCode, method, url, string(respBodyBytes))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff.Pause()):
			}
			continue
		}
		if resp.StatusCode == http.StatusPreconditionFailed {
			return storage.NewErrorf(storage.PreconditionFailed, "got %d-response to %s %s: %s", resp.StatusCode, method, url, string(respBodyBytes))
		}
		return fmt.Errorf("got unexpected %d-response to %s %s: %s", resp.StatusCode, method, url, string(respBodyBytes))
	}
	return nil
}

func (s *Storage) DeleteObject(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	err := s.gcsClientBucket.Object(name).Delete(ctx)
	if err != nil {
		return mapGCSPackageError(err)
	}
	return nil
}

func (s *Storage) GetObject(ctx context.Context, name string) (io.ReadCloser, error) {
	if name == "" {
		return nil, fmt.Errorf("name must not be empty")
	}
	// TODO set fields parameter to only include metadata in the response body
	data, err := s.gcsClientBucket.Object(name).NewReader(ctx)
	if err != nil {
		return nil, mapGCSPackageError(err)
	}
	return data, nil
}

func (s *Storage) GetObjectMetadata(ctx context.Context, name string) (storage.ObjectMetadata, error) {
	if name == "" {
		return nil, fmt.Errorf("name must not be empty")
	}
	objAttrs, err := s.gcsClientBucket.Object(name).Attrs(ctx)
	if err != nil {
		return nil, mapGCSPackageError(err)
	}
	return objAttrs.Metadata, nil
}

func (s *Storage) ListObjects(ctx context.Context, opts storage.ObjectListOptions) (*storage.ObjectList, error) {
	method := http.MethodGet
	urlQuery := url.Values{}
	if opts.MaxResults < 0 {
		return nil, fmt.Errorf("opts.MaxResults must be non-negative")
	}
	if opts.MaxResults > 0 {
		urlQuery.Set("maxResults", strconv.FormatInt(int64(opts.MaxResults), 10))
	}
	if opts.NamePrefix != "" {
		urlQuery.Set("prefix", opts.NamePrefix)
	}
	urlQuery.Set("fields", "items(name,timeDeleted),nextPageToken")
	urlQuery.Set("prettyPrint", "false")
	url := s.objectListURL
	if len(urlQuery) > 0 {
		url = url + "?" + urlQuery.Encode()
	}
	var respBodyReader io.ReadCloser
	defer func() {
		if respBodyReader != nil {
			err2 := respBodyReader.Close()
			if err2 != nil {
				log.Errorf("error closing body of response to %s %s: %v", method, url, err2)
			}
		}
	}()
	// Inspired by https://github.com/googleapis/google-cloud-go/blob/67b19f0bd698c1df21addff89060b4356816a4d3/storage/invoke.go#L26
	var backoff gax.Backoff
	for {
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating request %s %s: %w", method, url, err)
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			if !shouldRetryDoRequest(err) {
				return nil, fmt.Errorf("error doing request %s %s: %w", method, url, err)
			}
			log.Errorf("retrying because got intermittent error doing request %s %s: %v", method, url, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff.Pause()):
			}
			continue
		}
		respBodyReader = resp.Body
		if resp.StatusCode == http.StatusOK {
			respBody := &struct {
				Items []struct {
					Name        string `json:"name"`
					TimeDeleted string `json:"timeDeleted"`
				} `json:"items"`
				NextPageToken string `json:"nextPageToken"`
			}{}
			if err := util.UnmarshalJSON(respBodyReader, respBody, false); err != nil {
				return nil, fmt.Errorf("error unmarshalling body of %d-response to %s %s: %v", resp.StatusCode, method, url, err)
			}
			objList := &storage.ObjectList{
				NextPageToken: respBody.NextPageToken,
			}
			for _, item := range respBody.Items {
				if item.TimeDeleted == "" {
					objList.Names = append(objList.Names, item.Name)
				}
			}
			return objList, nil
		}
		respBodyBytes, err2 := io.ReadAll(respBodyReader)
		if err2 != nil {
			log.Errorf("error reading body of %d-response to %s %s: %v", resp.StatusCode, method, url, err2)
		}
		err2 = respBodyReader.Close()
		respBodyReader = nil
		if err2 != nil {
			log.Errorf("error closing body of %d-response to %s %s: %v", resp.StatusCode, method, url, err2)
		}
		if resp.StatusCode == http.StatusTooManyRequests || (500 <= resp.StatusCode && resp.StatusCode <= 599) {
			log.Errorf("retrying because got intermittent %d-response to %s %s: %s", resp.StatusCode, method, url, string(respBodyBytes))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff.Pause()):
			}
			continue
		}
		return nil, fmt.Errorf("got unexpected %d-response to %s %s: %s", resp.StatusCode, method, url, string(respBodyBytes))
	}
}

func mapGCSPackageError(err error) error {
	if err == gcs.ErrObjectNotExist {
		return storage.NewErrorf(storage.NotFound, err.Error(), err)
	}
	var googleAPIErr *googleapi.Error
	if errors.As(err, &googleAPIErr) {
		if googleAPIErr.Code == http.StatusNotFound {
			return storage.NewErrorf(storage.NotFound, err.Error(), err)
		}
	}
	return err
}

func shouldRetryDoRequest(err error) bool {
	// Inspired by https://github.com/googleapis/google-cloud-go/blob/67b19f0bd698c1df21addff89060b4356816a4d3/storage/not_go110.go#L36
	if strings.Contains(err.Error(), "REFUSED_STREAM") {
		return true
	} else if t, ok := err.(interface{ Temporary() bool }); ok && t.Temporary() {
		return true
	}
	return false
}
