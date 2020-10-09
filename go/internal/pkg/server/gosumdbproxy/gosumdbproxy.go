package gosumdbproxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/go-mod-proxy/go/internal/pkg/config"
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
		if sumDBElement.URLParsed.RawQuery != "" {
			return nil, fmt.Errorf("opts.SumDatabaseProxy.SumDatabases[%d].URLParsed.RawQuery must be empty", i)
		}
		if sumDBElement.URLParsed.ForceQuery {
			return nil, fmt.Errorf("opts.SumDatabaseProxy.SumDatabases[%d].URLParsed.ForceQuery must be false", i)
		}
		if sumDBElement.URLParsed.Fragment != "" {
			return nil, fmt.Errorf("opts.SumDatabaseProxy.SumDatabases[%d].URLParsed.Fragment must be empty", i)
		}
		reverseProxy := httputil.NewSingleHostReverseProxy(sumDBElement.URLParsed)
		reverseProxy.Transport = opts.Transport
		// TODO set reverseProxy.BufferPool to improve buffer sharing
		// TODO set reverseProxy.ErrorLog to log to logrus

		// reverseProxy does not retain URL path encoding
		// TODO use a reverse proxy that retains URL path encoding because in general it is better for reverse proxies to preserve
		// information
		s.sumdbs[sumDBElement.Name] = reverseProxy
	}
	opts.ParentRouter.PathPrefix("/sumdb/{x}").Methods(http.MethodGet).HandlerFunc(s.serveHTTP)
	return s, nil
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
	log.Tracef("%s %s (escapedPathSuffix = %#v)", req.Method, req.URL.String(), escapedPathSuffix)
	sumDBName := escapedPathSuffix
	if i := strings.IndexByte(escapedPathSuffix, '/'); i >= 0 {
		sumDBName = escapedPathSuffix[:i]
		escapedPathSuffix = escapedPathSuffix[i+1:]
	}
	log.Tracef("%s %s (sum database = %#v, escapedPathSuffix = %#v)", req.Method, req.URL.String(), sumDBName, escapedPathSuffix)
	reverseProxy := s.sumdbs[sumDBName]
	if reverseProxy == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if escapedPathSuffix == "supported" {
		w.WriteHeader(http.StatusOK)
		return
	}
	reqClone := req.Clone(req.Context())
	reqClone.URL.Path = url.PathEscape(escapedPathSuffix)
	reqClone.URL.RawPath = ""
	reverseProxy.ServeHTTP(w, reqClone)
}
