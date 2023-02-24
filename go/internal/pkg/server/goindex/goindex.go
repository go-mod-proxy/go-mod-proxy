package index

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	servicegoindex "github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/service/index"
)

type ServerOptions struct {
	IndexService *servicegoindex.Service
	Router       *mux.Router
}

type Server struct {
	service *servicegoindex.Service
	router  *mux.Router
}

func NewServer(opts ServerOptions) *Server {
	s := &Server{
		service: opts.IndexService,
		router:  opts.Router,
	}
	s.router.PathPrefix("/index").Methods(http.MethodGet).Handler(s)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var since time.Time
	var limit int
	if sinceQuery, err := time.Parse(time.RFC3339, req.URL.Query().Get("since")); err != nil {
		since = sinceQuery
	}
	if limitQuery, err := strconv.Atoi(req.URL.Query().Get("limit")); err != nil {
		limit = limitQuery
	}

	modules, err := s.service.GetIndex(req.Context(), since, limit)
	if err != nil {
		status := http.StatusInternalServerError
		log.Tracef("responding to index request with %s due to error: %v", status, err)
		w.WriteHeader(status)
		return
	}

	for _, module := range modules {
		jsonl, err := json.Marshal(module)
		if err != nil {
			status := http.StatusInternalServerError
			log.Tracef("failed to marshal module %+v due to error: %v", module, err)
			w.WriteHeader(status)
			return
		}
		w.Write(jsonl)
	}
}
