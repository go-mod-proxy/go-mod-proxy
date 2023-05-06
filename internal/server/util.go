package server

import (
	"net/http"

	jasperhttp "github.com/jbrekelmans/go-lib/http"
	log "github.com/sirupsen/logrus"

	servercommon "github.com/go-mod-proxy/go-mod-proxy/internal/pkg/server/common"
)

func responseUnauthorized(w http.ResponseWriter, realm string) {
	wwwAuthenticateErr, err := jasperhttp.NewWWWAuthenticateError("", []*jasperhttp.Challenge{
		{
			Scheme: jasperhttp.AuthenticationSchemeBearer,
			Params: []*jasperhttp.Param{
				{
					Attribute: "realm",
					Value:     realm,
				},
			},
		},
	})
	if err != nil {
		log.Errorf("error formatting %s response header: %v", jasperhttp.HeaderNameWWWAuthenticate, err)
		servercommon.InternalServerError(w)
		return
	}
	headerValue, err := wwwAuthenticateErr.HeaderValue(realm)
	if err != nil {
		log.Errorf("error formatting %s %s response header: %v", jasperhttp.HeaderNameWWWAuthenticate,
			jasperhttp.AuthenticationSchemeBearer, err)
		servercommon.InternalServerError(w)
		return
	}
	w.Header().Add(jasperhttp.HeaderNameWWWAuthenticate, headerValue)
	http.Error(w, wwwAuthenticateErr.Error(), http.StatusUnauthorized)
}
