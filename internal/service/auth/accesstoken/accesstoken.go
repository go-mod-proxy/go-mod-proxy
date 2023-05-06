package accesstoken

import (
	"context"
	"fmt"
	"time"

	jasperauth "github.com/jbrekelmans/go-lib/auth"
	jasperhttp "github.com/jbrekelmans/go-lib/http"
	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/jwt"

	"github.com/go-mod-proxy/go-mod-proxy/internal/service/auth"
)

type Authenticator struct {
	audience      string
	timeToLive    time.Duration
	identityStore auth.IdentityStore
	secret        []byte
	signer        jose.Signer
}

func NewAuthenticator(audience string, secret []byte, timeToLive time.Duration, identityStore auth.IdentityStore) (*Authenticator, error) {
	if identityStore == nil {
		return nil, fmt.Errorf("identityStore must not be nil")
	}
	a := &Authenticator{
		audience:      audience,
		timeToLive:    timeToLive,
		identityStore: identityStore,
		secret:        secret,
	}
	var err error
	a.signer, err = jose.NewSigner(jose.SigningKey{
		Algorithm: jose.HS256,
		Key:       secret,
	}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Authenticator) Authenticate(ctx context.Context, bearerToken string) (any, error) {
	jwtParsed, err := jwt.ParseSigned(bearerToken)
	if err != nil {
		return nil, jasperhttp.ErrorInvalidBearerToken(fmt.Sprintf("invalid token: %v", err))
	}
	claims := &jwt.Claims{}
	err = jwtParsed.Claims(a.secret, &claims)
	if err != nil {
		return nil, jasperhttp.ErrorInvalidBearerToken(fmt.Sprintf("invalid token: %v", err))
	}
	err = claims.ValidateWithLeeway(jwt.Expected{
		Audience: jwt.Audience{a.audience},
		Time:     time.Now(),
	}, jasperauth.DefaultJWTClaimsLeeway)
	if err != nil {
		return nil, jasperhttp.ErrorInvalidBearerToken(fmt.Sprintf("invalid token: %v", err))
	}
	identity, err := a.identityStore.FindByName(claims.Subject)
	if err != nil {
		if err == auth.ErrNotFound {
			return nil, jasperhttp.ErrorInvalidBearerToken(fmt.Sprintf("no identity exists named %s", claims.Subject))
		}
		return nil, err
	}
	return identity, nil
}

func (a *Authenticator) Issue(identity *auth.Identity) (accessToken string, err error) {
	if identity == nil {
		return "", fmt.Errorf("identity must not be nil")
	}
	claims := jwt.Claims{
		Subject:  identity.Name,
		Audience: jwt.Audience{a.audience},
		Expiry:   jwt.NewNumericDate(time.Now().Add(a.timeToLive)),
	}
	accessToken, err = jwt.Signed(a.signer).Claims(claims).CompactSerialize()
	return
}

func (a *Authenticator) TimeToLive() time.Duration {
	return a.timeToLive
}
