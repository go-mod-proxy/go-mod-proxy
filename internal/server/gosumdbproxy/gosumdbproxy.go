package gosumdbproxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/go-mod-proxy/go-mod-proxy/internal/config"
)

type ServerOptions struct {
	// UseEncodedPath must have been called on ParentRouter for correct routing.
	ParentRouter     *mux.Router
	SumDatabaseProxy *config.SumDatabaseProxy
	Transport        http.RoundTripper
}

type Server struct {
	discourageClientDirectSumDatabaseConnections bool
	sumdbs                                       map[string]*httputil.ReverseProxy
}

func NewServer(opts ServerOptions) (*Server, error) {
	if opts.Transport == nil {
		return nil, fmt.Errorf("opts.Transport must not be nil")
	}
	s := &Server{
		discourageClientDirectSumDatabaseConnections: opts.SumDatabaseProxy.DiscourageClientDirectSumDatabaseConnections,
		sumdbs: make(map[string]*httputil.ReverseProxy, len(opts.SumDatabaseProxy.SumDatabases)),
	}
	for i, sumDBElement := range opts.SumDatabaseProxy.SumDatabases {
		// Perform these checks because we don't want to rely on reverseProxy semantics
		urlParsed := sumDBElement.URLParsed
		if urlParsed.Fragment != "" {
			return nil, fmt.Errorf("opts.SumDatabaseProxy.SumDatabases[%d].URLParsed.Fragment must be empty", i)
		}
		reverseProxy := httputil.NewSingleHostReverseProxy(urlParsed)
		director1 := reverseProxy.Director
		director2 := func(req *http.Request) {
			director1(req)
			req.Host = urlParsed.Host
		}
		reverseProxy.Director = director2
		reverseProxy.ModifyResponse = s.reverseProxyModifyResponse
		reverseProxy.Transport = opts.Transport
		// TODO set reverseProxy.BufferPool to improve buffer sharing
		// TODO set reverseProxy.ErrorLog to log to logrus
		s.sumdbs[sumDBElement.Name] = reverseProxy
	}
	opts.ParentRouter.PathPrefix("/sumdb/{x:.*}").Methods(http.MethodGet).HandlerFunc(s.serveHTTP)
	return s, nil
}

func (s *Server) reverseProxyModifyResponse(res *http.Response) error {
	if log.IsLevelEnabled(log.TraceLevel) {
		req := res.Request
		log.Tracef("sumdb proxy: sumdb responded to %s %s with status %d", req.Method, req.URL.String(), res.StatusCode)
	}
	return nil
}

func (s *Server) serveHTTP(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	escapedPathSuffix := vars["x"]
	if escapedPathSuffix == "supported" {
		log.Tracef("%s %s", req.Method, req.URL.String())
		if s.discourageClientDirectSumDatabaseConnections {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusGone)
		return
	}
	sumDBName := escapedPathSuffix
	if i := strings.IndexByte(escapedPathSuffix, '/'); i >= 0 {
		sumDBName = escapedPathSuffix[:i]
		escapedPathSuffix = escapedPathSuffix[i:]
	} else {
		escapedPathSuffix = ""
	}
	reverseProxy := s.sumdbs[sumDBName]
	if reverseProxy == nil {
		status := http.StatusNotFound
		log.Tracef("responding to %s %s (sum database = %#v) with status %d because the sum database is unknown", req.Method, req.URL.String(),
			sumDBName, status)
		w.WriteHeader(status)
		return
	}
	if escapedPathSuffix == "/supported" {
		status := http.StatusOK
		log.Tracef("responding to %s %s (sum database = %#v) with status %d", req.Method, req.URL.String(),
			sumDBName, status)
		w.WriteHeader(status)
		return
	}
	reqClone := req.Clone(req.Context())
	// We only have to modify the path of reqClone.URL because reverseProxy.Director does the rest.
	pathSuffix, err := url.PathUnescape(escapedPathSuffix)
	if err != nil {
		status := http.StatusNotFound
		log.Tracef("responding to %s %s (sum database = %#v) with status %d due to error unescaping path suffix: %v", req.Method, req.URL.String(),
			sumDBName, status, err)
		w.WriteHeader(status)
		return
	}
	reqClone.URL.Path = pathSuffix
	reqClone.URL.RawPath = escapedPathSuffix
	reverseProxy.ServeHTTP(w, reqClone)
}
