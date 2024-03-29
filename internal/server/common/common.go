package common

import (
	"net/http"

	"github.com/go-mod-proxy/go-mod-proxy/internal/service/auth"
)

type RequestAuthenticatorFunc = func(w http.ResponseWriter, req *http.Request) *auth.Identity

func InternalServerError(w http.ResponseWriter) {
	code := http.StatusInternalServerError
	http.Error(w, http.StatusText(code), code)
}
