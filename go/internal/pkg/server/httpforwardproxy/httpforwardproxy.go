package httpforwardproxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

type ServerOptions struct {
	ParentHTTPProxyFunc func(*url.URL) (*url.URL, error)
}

type Server struct {
	httpServer          http.Server
	listener            net.Listener
	parentHTTPProxyFunc func(*url.URL) (*url.URL, error)
	router              *mux.Router
}

func NewServer(opts ServerOptions) (*Server, error) {
	if opts.ParentHTTPProxyFunc == nil {
		return nil, fmt.Errorf("opts.ParentHTTPProxyFunc must not be nil")
	}
	s := &Server{
		parentHTTPProxyFunc: opts.ParentHTTPProxyFunc,
	}
	s.router = mux.NewRouter().UseEncodedPath().SkipClean(true)
	s.httpServer.Handler = s.router
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) logf(logLevel log.Level, format string, args ...interface{}) {
	logger := log.StandardLogger()
	if !logger.IsLevelEnabled(logLevel) {
		return
	}
	log.NewEntry(logger).Logf(logLevel, "credentialhelper.Server: "+format, args...)
}

func (s *Server) serve() {
	err := s.httpServer.Serve(s.listener)
	if err != nil && err != http.ErrServerClosed {
		s.logf(log.ErrorLevel, "(*http.Server).Serve returned error: %v", err)
	} else {
		s.logf(log.InfoLevel, "(*http.Server).Serve returned gracefully")
	}
}

func (s *Server) Start(ctx context.Context, port int) error {
	listenConfig := net.ListenConfig{}
	network := "tcp"
	address := fmt.Sprintf("127.0.0.1:%d", port)
	var err error
	s.listener, err = listenConfig.Listen(ctx, network, address)
	if err != nil {
		return err
	}
	s.logf(log.InfoLevel, "listening on %s/%s", network, address)
	go s.serve()
	return nil
}

func (s *Server) Stop() {
	ctx := context.Background()
	ctx, cancelFunc := context.WithTimeout(ctx, time.Second*10)
	defer cancelFunc()
	err := s.httpServer.Shutdown(ctx)
	if err != nil {
		s.logf(log.ErrorLevel, `calling (*http.Server).Close because (*http.Server).Shutdown returned error: %v`, err)
		err = s.httpServer.Close()
		if err != nil {
			s.logf(log.ErrorLevel, `(*http.Server).Close returned error: %v`, err)
		} else {
			s.logf(log.InfoLevel, `(*http.Server).Close returned without error`)
		}
	} else {
		s.logf(log.InfoLevel, `(*http.Server).Shutdown returned without error`)
	}
}
