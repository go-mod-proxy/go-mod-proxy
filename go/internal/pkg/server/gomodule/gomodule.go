package module

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"golang.org/x/mod/module"

	"github.com/jbrekelmans/go-module-proxy/internal/pkg/config"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/server/common"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/service/auth"
	servicegomodule "github.com/jbrekelmans/go-module-proxy/internal/pkg/service/gomodule"
)

const (
	endOfModulePathInRequestURIPath = "/@"
	headerNameContentType           = "Content-Type"
	contentTypeInfo                 = "application/json"
	contentTypeText                 = "text/plain; charset=UTF-8"
	contentTypeZip                  = "application/zip"
)

func getVersion(rw http.ResponseWriter, versionRaw string) (string, bool) {
	version, err := module.UnescapeVersion(versionRaw)
	if err != nil {
		// 410 for consistency with proxy.golang.org
		http.Error(rw, fmt.Sprintf("error parsing version %#v in requestURI path: %v", versionRaw, err), http.StatusGone)
		return "", false
	}
	return version, true
}

type ServerOptions struct {
	AccessControlList    []*config.AccessControlListElement
	ClientAuthEnabled    bool
	GoModuleService      servicegomodule.Service
	ModuleRewriteRules   []*config.ModuleRewriteRule
	RequestAuthenticator common.RequestAuthenticatorFunc
	Router               *mux.Router
}

// Server implements the Go module proxy protocol: https://golang.org/cmd/go/#hdr-Module_proxy_protocol.
type Server struct {
	acl                  []*config.AccessControlListElement
	clientAuthEnabled    bool
	moduleRewriteRules   []*config.ModuleRewriteRule
	goModuleService      servicegomodule.Service
	requestAuthenticator common.RequestAuthenticatorFunc
	router               *mux.Router
}

// NewServer is a constructor for Server.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.GoModuleService == nil {
		return nil, fmt.Errorf("opts.GoModuleService must not be nil")
	}
	if opts.RequestAuthenticator == nil {
		return nil, fmt.Errorf("opts.RequestAuthenticator must not be nil")
	}
	if opts.Router == nil {
		return nil, fmt.Errorf("opts.Router must not be nil")
	}
	s := &Server{
		acl:                  opts.AccessControlList,
		clientAuthEnabled:    opts.ClientAuthEnabled,
		moduleRewriteRules:   opts.ModuleRewriteRules,
		goModuleService:      opts.GoModuleService,
		requestAuthenticator: opts.RequestAuthenticator,
		router:               opts.Router,
	}
	s.router.PathPrefix("/").Methods(http.MethodGet).Handler(s)
	return s, nil
}

func (s *Server) applyRewriteRules(modulePath string) string {
	for _, rule := range s.moduleRewriteRules {
		match := rule.Regexp.Value.FindStringSubmatchIndex(modulePath)
		if match != nil {
			modulePathRewritten := rule.Regexp.Value.ExpandString(nil, rule.Replacement, modulePath, match)
			return string(modulePathRewritten)
		}
	}
	return modulePath
}

func (s *Server) authorize(identity *auth.Identity, modulePath string) config.Access {
	for _, aclElem := range s.acl {
		if len(aclElem.Identities) > 0 {
			found := false
			for _, identityName := range aclElem.Identities {
				if identity.Name == identityName {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		if aclElem.ModuleRegexp.Value != nil && !aclElem.ModuleRegexp.Value.MatchString(modulePath) {
			continue
		}
		return aclElem.Access
	}
	return config.AccessDeny
}

func (s *Server) latest(rw http.ResponseWriter, req *http.Request, modulePath string) {
	info, err := s.goModuleService.Latest(req.Context(), modulePath)
	if err != nil {
		log.Errorf("error getting info for latest version of module %s: %v", modulePath, err)
		http.Error(rw, fmt.Sprintf("error while getting info for latest version of module %s", modulePath), http.StatusInternalServerError)
		return
	}
	infoJSONBytes, err := json.Marshal(info)
	if err != nil {
		log.Errorf("error marshalling info of latest version of module %s: %v", modulePath, err)
		http.Error(rw, fmt.Sprintf("error marshalling info of latest version of module %s", modulePath), http.StatusInternalServerError)
		return
	}
	rw.Header().Set(headerNameContentType, contentTypeInfo)
	// name can be set to the empty string since it is only used to auto-detect content type.
	http.ServeContent(rw, req, "", time.Time{}, bytes.NewReader(infoJSONBytes))
}

func (s *Server) list(rw http.ResponseWriter, req *http.Request, modulePath string) {
	d, err := s.goModuleService.List(req.Context(), modulePath)
	if err != nil {
		log.Errorf("error listing versions of module %s: %v", modulePath, err)
		http.Error(rw, fmt.Sprintf("error listing versions of module %s", modulePath), http.StatusInternalServerError)
		return
	}
	defer func() {
		err := d.Close()
		if err != nil {
			log.Errorf("error closing %T: %v", d, err)
		}
	}()
	rw.Header().Set(headerNameContentType, contentTypeText)
	rw.WriteHeader(http.StatusOK)
	_, _ = io.Copy(rw, d)
}

func (s *Server) info(rw http.ResponseWriter, req *http.Request, modulePath, versionRaw string) {
	version, ok := getVersion(rw, versionRaw)
	if !ok {
		return
	}
	moduleVersion := module.Version{Path: modulePath, Version: version}
	info, err := s.goModuleService.Info(req.Context(), &moduleVersion)
	if err != nil {
		if servicegomodule.ErrorIsCode(err, servicegomodule.NotFound) {
			code := http.StatusNotFound
			http.Error(rw, http.StatusText(code), code)
			return
		}
		log.Errorf("error getting info for module %s: %v", moduleVersion.String(), err)
		http.Error(rw, fmt.Sprintf("error while getting info for module %s", moduleVersion.String()), http.StatusInternalServerError)
		return
	}
	infoJSONBytes, err := json.Marshal(info)
	if err != nil {
		log.Errorf("error marshalling info of latest version of module %s: %v", modulePath, err)
		http.Error(rw, fmt.Sprintf("error marshalling info of latest version of module %s", modulePath), http.StatusInternalServerError)
		return
	}
	rw.Header().Set(headerNameContentType, contentTypeInfo)
	// name can be set to the empty string since it is only used to auto-detect content type.
	http.ServeContent(rw, req, "", time.Time{}, bytes.NewReader(infoJSONBytes))
}

func (s *Server) goMod(rw http.ResponseWriter, req *http.Request, modulePath, versionRaw string) {
	version, ok := getVersion(rw, versionRaw)
	if !ok {
		return
	}
	moduleVersion := module.Version{Path: modulePath, Version: version}
	d, err := s.goModuleService.GoMod(req.Context(), &moduleVersion)
	if err != nil {
		if servicegomodule.ErrorIsCode(err, servicegomodule.NotFound) {
			code := http.StatusNotFound
			http.Error(rw, http.StatusText(code), code)
			return
		}
		log.Errorf("error getting mod file for module %s: %v", moduleVersion.String(), err)
		http.Error(rw, fmt.Sprintf("error while getting mod file for module %s", moduleVersion.String()), http.StatusInternalServerError)
		return
	}
	defer func() {
		err := d.Close()
		if err != nil {
			log.Errorf("error closing %T: %v", d, err)
		}
	}()
	rw.Header().Set(headerNameContentType, contentTypeText)
	rw.WriteHeader(http.StatusOK)
	_, _ = io.Copy(rw, d)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "only HTTP method GET is allowed", http.StatusMethodNotAllowed)
		return
	}
	pathRest := req.URL.Path
	i := strings.Index(pathRest, endOfModulePathInRequestURIPath)
	if i < 0 {
		// 410 for consistency with proxy.golang.org
		http.Error(w, "requestURI path must contain a component starting with an @ character", http.StatusGone)
		return
	}
	modulePath, err := module.UnescapePath(strings.TrimPrefix(pathRest[:i], "/"))
	if err != nil {
		// 410 for consistency with proxy.golang.org
		http.Error(w, fmt.Sprintf("requestURI path is invalid: the substring representing the module is incorrectly encoded: %v", err),
			http.StatusGone)
		return
	}
	if s.clientAuthEnabled {
		identity := s.requestAuthenticator(w, req)
		if identity == nil {
			return
		}
		modulePath = s.applyRewriteRules(modulePath)
		access := s.authorize(identity, modulePath)
		if access == config.AccessDeny {
			http.Error(w, "module does not exist, that's all we know.", http.StatusNotFound)
			return
		}
	}
	pathRest = pathRest[i+len(endOfModulePathInRequestURIPath):]
	switch pathRest {
	case "latest":
		s.latest(w, req, modulePath)
		return
	case "v/list":
		s.list(w, req, modulePath)
		return
	default:
		if !strings.HasPrefix(pathRest, "v/") {
			// 410 for consistency with proxy.golang.org
			http.Error(w, "requestURI path is invalid", http.StatusGone)
			return
		}
		pathRest = pathRest[2:]
		i = strings.LastIndexByte(pathRest, '.')
		if i < 0 {
			// 410 for consistency with proxy.golang.org
			http.Error(w, fmt.Sprintf("no file extension in filename %#v", pathRest), http.StatusGone)
			return
		}
		ext := pathRest[i+1:]
		switch ext {
		case "info":
			s.info(w, req, modulePath, pathRest[:i])
			return
		case "mod":
			s.goMod(w, req, modulePath, pathRest[:i])
			return
		case "zip":
			s.zip(w, req, modulePath, pathRest[:i])
			return
		}
		// 410 for consistency with proxy.golang.org
		http.Error(w, fmt.Sprintf("unexpected extension %#v", ext), http.StatusGone)
	}
}

func (s *Server) zip(rw http.ResponseWriter, req *http.Request, modulePath, versionRaw string) {
	version, ok := getVersion(rw, versionRaw)
	if !ok {
		return
	}
	moduleVersion := module.Version{Path: modulePath, Version: version}
	d, err := s.goModuleService.Zip(req.Context(), &moduleVersion)
	if err != nil {
		if servicegomodule.ErrorIsCode(err, servicegomodule.NotFound) {
			code := http.StatusNotFound
			http.Error(rw, http.StatusText(code), code)
			return
		}
		log.Errorf("error getting zip file for module %s: %v", moduleVersion.String(), err)
		http.Error(rw, fmt.Sprintf("error getting zip file for module %s", moduleVersion.String()), http.StatusInternalServerError)
		return
	}
	defer func() {
		err := d.Close()
		if err != nil {
			log.Errorf("error closing %T: %v", d, err)
		}
	}()
	rw.Header().Set(headerNameContentType, contentTypeZip)
	rw.WriteHeader(http.StatusOK)
	_, _ = io.Copy(rw, d)
}
