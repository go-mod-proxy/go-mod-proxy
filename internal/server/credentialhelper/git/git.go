package git

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/go-mod-proxy/go-mod-proxy/internal/config"
	"github.com/go-mod-proxy/go-mod-proxy/internal/github"
	"github.com/go-mod-proxy/go-mod-proxy/internal/util"
)

type ServerOptions struct {
	GitHubClientManager *github.GitHubClientManager
	// UseEncodedPath must have been called on ParentRouter.
	ParentRouter   *mux.Router
	PrivateModules []*config.PrivateModulesElement
}

type Server struct {
	gitHubClientManager *github.GitHubClientManager
	privateModules      []*config.PrivateModulesElement
}

func NewServer(opts ServerOptions) (*Server, error) {
	if opts.GitHubClientManager == nil {
		return nil, fmt.Errorf("opts.GitHubClientManager must not be nil")
	}
	if opts.ParentRouter == nil {
		return nil, fmt.Errorf("opts.ParentRouter must not be nil")
	}
	s := &Server{
		gitHubClientManager: opts.GitHubClientManager,
		privateModules:      opts.PrivateModules,
	}
	opts.ParentRouter.Path("/git").Methods(http.MethodPost).HandlerFunc(s.post)
	return s, nil
}

func (s *Server) post(w http.ResponseWriter, req *http.Request) {
	var goModulePath string
	if err := util.UnmarshalJSON(req.Body, &goModulePath, true); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var privateModulesElement2 *config.PrivateModulesElement
	for _, privateModulesElement1 := range s.privateModules {
		if util.PathIsLexicalDescendant(goModulePath, privateModulesElement1.PathPrefix) {
			privateModulesElement2 = privateModulesElement1
			break
		}
	}
	if privateModulesElement2 == nil {
		http.Error(w, fmt.Sprintf(`Go module path (%#v) is not known to be a private module`, goModulePath), http.StatusInternalServerError)
		return
	}
	if privateModulesElement2.Auth.GitHubApp != nil {
		goModulePathParts := strings.SplitN(goModulePath, "/", 3)
		if len(goModulePathParts) < 2 {
			http.Error(w, `Go module path (%#v) must contain a "/"`, http.StatusBadRequest)
			return
		}
		host, repoOwner := goModulePathParts[0], goModulePathParts[1]
		_, tokenGetter, err := s.gitHubClientManager.GetGitHubAppClient(req.Context(), host, *privateModulesElement2.Auth.GitHubApp, repoOwner)
		if err != nil {
			if _, ok := err.(*github.NotDefinedError); ok {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			log.Error(err)
			code := http.StatusInternalServerError
			http.Error(w, http.StatusText(code), code)
			return
		}
		token, err := tokenGetter(req.Context())
		if err != nil {
			log.Error(err)
			code := http.StatusInternalServerError
			http.Error(w, http.StatusText(code), code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(UserPassword{
			User:     "x-access-token",
			Password: token,
		})
		return
	}
	error := fmt.Sprintf("TODO implement a mechanism to retrieve credentials for non-GitHub repositories (%#v)", goModulePath)
	log.Debugf("git credential helper server: %s", error)
	http.Error(w, error, http.StatusInternalServerError)
}

type UserPassword struct {
	User     string `json:"user"`
	Password string `json:"password"`
}
