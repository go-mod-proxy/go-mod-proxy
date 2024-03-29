package server

import (
	"crypto/subtle"
	"net/http"

	log "github.com/sirupsen/logrus"

	internalErrors "github.com/go-mod-proxy/go-mod-proxy/internal/errors"
	servercommon "github.com/go-mod-proxy/go-mod-proxy/internal/server/common"
	"github.com/go-mod-proxy/go-mod-proxy/internal/util"
)

func (s *Server) authenticateUserPassword(w http.ResponseWriter, req *http.Request) {
	log.Tracef("received HTTP request on user-password authentication endpoint")
	var reqBody struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := util.UnmarshalJSON(req.Body, &reqBody, true); err != nil {
		log.Trace(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	authenticatedIdentity, err := s.identityStore.FindByName(reqBody.User)
	if err != nil {
		if internalErrors.ErrorIsCode(err, internalErrors.NotFound) {
			responseUnauthorized(w, s.realm)
			return
		}
		log.Error(err)
		servercommon.InternalServerError(w)
		return
	}
	if authenticatedIdentity.Password == nil ||
		subtle.ConstantTimeCompare(authenticatedIdentity.Password.Plaintext, []byte(reqBody.Password)) == 0 {
		responseUnauthorized(w, s.realm)
		return
	}
	s.serveHTTPIssueToken(w, authenticatedIdentity)
}
