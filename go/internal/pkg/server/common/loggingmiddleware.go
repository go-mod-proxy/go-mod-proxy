package common

import (
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

type instrumentedReadCloser struct {
	totalRead  int
	readCloser io.ReadCloser
}

var _ io.ReadCloser = (*instrumentedReadCloser)(nil)

func newInstrumentedReadCloser(readCloser io.ReadCloser) *instrumentedReadCloser {
	return &instrumentedReadCloser{
		readCloser: readCloser,
	}
}

func (i *instrumentedReadCloser) Close() error {
	return i.readCloser.Close()
}

func (i *instrumentedReadCloser) Read(p []byte) (n int, err error) {
	n, err = i.readCloser.Read(p)
	i.totalRead += n
	return
}

type instrumentedResponseWriter struct {
	writeHeaderCalled bool
	statusCode        int
	totalWritten      int
	w                 http.ResponseWriter
}

var _ http.ResponseWriter = (*instrumentedResponseWriter)(nil)

func newInstrumentedResponseWriter(w http.ResponseWriter) *instrumentedResponseWriter {
	return &instrumentedResponseWriter{
		w: w,
	}
}

func (i *instrumentedResponseWriter) Header() http.Header {
	return i.Header()
}

func (i *instrumentedResponseWriter) WriteHeader(statusCode int) {
	i.writeHeaderCalled = true
	i.statusCode = statusCode
	i.w.WriteHeader(statusCode)
}

func (i *instrumentedResponseWriter) Write(p []byte) (n int, err error) {
	n, err = i.w.Write(p)
	i.totalWritten += n
	return
}

func LoggingMiddleware(logger *log.Logger, lvl log.Level, prefix string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !logger.IsLevelEnabled(lvl) {
				next.ServeHTTP(w, req)
				return
			}
			reqClone := req.Clone(req.Context())
			instrumentedBody := newInstrumentedReadCloser(req.Body)
			reqClone.Body = instrumentedBody
			// NOTE: it is safe to assume req.GetBody is unused since we are dealing with server requests.

			instrumentedResponseWriter := newInstrumentedResponseWriter(w)

			start := time.Now()
			next.ServeHTTP(instrumentedResponseWriter, reqClone)
			elapsedTime := time.Since(start)

			// If instrumentedResponseWriter.Write is called then instrumentedResponseWriter.WriteHeader will be called (by definition of http.ResponseWriter interface)
			// but if instrumentedResponseWriter.Write and instrumentedResponseWriter.WriteHeader are both never called this equates to statusCode 200 OK
			// with an empty body: https://github.com/golang/go/blob/acb189ea59d7f47e5db075e502dcce5eac6571dc/src/net/http/server.go#L1621

			statusCode := instrumentedResponseWriter.statusCode
			if !instrumentedResponseWriter.writeHeaderCalled {
				statusCode = http.StatusOK
			}

			// instrumentedBody.totalRead = number of bytes in the request body after decoding transfer (i.e. "Transfer-Encoding: chunked")
			//		but before any decompression (i.e. "Content-Encoding"). In case of errors from the underlying transport
			//      (i.e. client closes connection) the number may be less than what the client sent.
			//
			// instrumentedResponseWriter.totalWritten = number of bytes in the response body before transfer encoding and after compression
			//		(assuming this middleware is run before any compression middleware).

			path := req.URL.EscapedPath()
			var pathAndQuery strings.Builder
			pathAndQuery.Grow(len(path) + len(req.URL.RawQuery) + 1)
			pathAndQuery.WriteString(path)
			if req.URL.ForceQuery || req.URL.RawQuery != "" {
				pathAndQuery.WriteByte('?')
			}
			pathAndQuery.WriteString(req.URL.RawQuery)

			// X-Forwarded-For is a common header respected by many load-balancers.
			// The idea is that whenever a load-balancer forwards a request it appends the
			// client IP address to the list in the X-Forwarded-For header. If all load-balancers between a client and server
			// follow this behaviour then the first element in the list is the client IP address as seen by the first load-balancer.
			var xForwardedFor []string
			for _, x := range req.Header.Values("X-Forwarded-For") {
				for _, xElem := range strings.Split(",", x) {
					xElemTrimmed := strings.Trim(xElem, " ")
					xForwardedFor = append(xForwardedFor, xElemTrimmed)
				}
			}
			log.NewEntry(logger).Logf(lvl, "%s: %s %#v status=%d duration=%dms requestBodySize=%d responseBodySize=%d xForwardedFor=%#v remoteAddr=%#v",
				prefix, req.Method, pathAndQuery.String(), statusCode, elapsedTime.Milliseconds(), instrumentedBody.totalRead, instrumentedResponseWriter.totalWritten,
				strings.Join(xForwardedFor, ", "), req.RemoteAddr)
		})
	}
}
