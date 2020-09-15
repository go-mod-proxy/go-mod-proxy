package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	jasperhttp "github.com/jbrekelmans/go-lib/http"
	log "github.com/sirupsen/logrus"
	"gopkg.in/square/go-jose.v2/json"

	"github.com/go-mod-proxy/go/internal/pkg/config"
	servercommon "github.com/go-mod-proxy/go/internal/pkg/server/common"
	servergomodule "github.com/go-mod-proxy/go/internal/pkg/server/gomodule"
	servergosumdbproxy "github.com/go-mod-proxy/go/internal/pkg/server/gosumdbproxy"
	serviceauth "github.com/go-mod-proxy/go/internal/pkg/service/auth"
	serviceauthaccesstoken "github.com/go-mod-proxy/go/internal/pkg/service/auth/accesstoken"
	serviceauthgce "github.com/go-mod-proxy/go/internal/pkg/service/auth/gce"
	servicegomodule "github.com/go-mod-proxy/go/internal/pkg/service/gomodule"
)

type ServerOptions struct {
	AccessControlList        []*config.AccessControlListElement
	AccessTokenAuthenticator *serviceauthaccesstoken.Authenticator
	GCEAuthenticator         *serviceauthgce.Authenticator
	ClientAuthEnabled        bool
	GoModuleService          servicegomodule.Service
	IdentityStore            serviceauth.IdentityStore
	ModuleRewriteRules       []*config.ModuleRewriteRule
	Realm                    string
	SumDatabaseProxy         *config.SumDatabaseProxy
	Transport                http.RoundTripper
}

type Server struct {
	accessTokenAuthenticator *serviceauthaccesstoken.Authenticator
	identityStore            serviceauth.IdentityStore
	realm                    string
	router                   *mux.Router
}

func NewServer(opts ServerOptions) (*Server, error) {
	if opts.AccessTokenAuthenticator == nil {
		return nil, fmt.Errorf("opts.AccessTokenAuthenticator must not be nil")
	}
	if opts.GoModuleService == nil {
		return nil, fmt.Errorf("opts.GoModuleService must not be nil")
	}
	if opts.IdentityStore == nil {
		return nil, fmt.Errorf("opts.IdentityStore must not be nil")
	}
	if opts.SumDatabaseProxy == nil {
		return nil, fmt.Errorf("opts.SumDatabaseProxy must not be nil")
	}
	if opts.Transport == nil {
		return nil, fmt.Errorf("opts.Transport must not be nil")
	}
	accessTokenAuthenticator, err := jasperhttp.NewBearerAuthorizer(opts.Realm, opts.AccessTokenAuthenticator.Authenticate)
	if err != nil {
		return nil, err
	}
	accessTokenAuthenticatorFunc := func(w http.ResponseWriter, req *http.Request) *serviceauth.Identity {
		identityRaw := accessTokenAuthenticator.Authorize(w, req)
		if identityRaw == nil {
			return nil
		}
		return identityRaw.(*serviceauth.Identity)
	}
	router := mux.NewRouter().UseEncodedPath().SkipClean(true)
	s := &Server{
		accessTokenAuthenticator: opts.AccessTokenAuthenticator,
		identityStore:            opts.IdentityStore,
		realm:                    opts.Realm,
		router:                   router,
	}
	authRouter := s.router.PathPrefix("/auth/").Subrouter()
	authRouter.Path("/userpassword").Methods(http.MethodPost).HandlerFunc(s.authenticateUserPassword)
	if opts.GCEAuthenticator != nil {
		gceBearerAuth, err := jasperhttp.NewBearerAuthorizer(opts.Realm, opts.GCEAuthenticator.Authenticate)
		if err != nil {
			return nil, err
		}
		authRouter.Path("/gce").Methods(http.MethodPost).HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			data := gceBearerAuth.Authorize(w, req)
			if data == nil {
				return
			}
			authenticatedIdentity := data.(*serviceauth.Identity)
			s.serveHTTPIssueToken(w, authenticatedIdentity)
		})
	}
	_, err = servergosumdbproxy.NewServer(servergosumdbproxy.ServerOptions{
		ParentRouter:     router,
		SumDatabaseProxy: opts.SumDatabaseProxy,
		Transport:        opts.Transport,
	})
	if err != nil {
		return nil, err
	}
	_, err = servergomodule.NewServer(servergomodule.ServerOptions{
		AccessControlList:    opts.AccessControlList,
		ClientAuthEnabled:    opts.ClientAuthEnabled,
		GoModuleService:      opts.GoModuleService,
		ModuleRewriteRules:   opts.ModuleRewriteRules,
		RequestAuthenticator: accessTokenAuthenticatorFunc,
		Router:               s.router,
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.router.ServeHTTP(w, req)
}

func (s *Server) serveHTTPIssueToken(w http.ResponseWriter, authenticatedIdentity *serviceauth.Identity) {
	accessToken, err := s.accessTokenAuthenticator.Issue(authenticatedIdentity)
	if err != nil {
		log.Errorf("error issueing access token: %v", err)
		servercommon.InternalServerError(w)
		return
	}
	expiresIn := int64(s.accessTokenAuthenticator.TimeToLive() / time.Second)
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(200)
	_ = json.NewEncoder(w).Encode(struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}{
		AccessToken: accessToken,
		ExpiresIn:   expiresIn,
		TokenType:   jasperhttp.AuthenticationSchemeBearer,
	})
}
