package gce

import (
	"context"
	"fmt"

	jaspercompute "github.com/jbrekelmans/go-lib/auth/google/compute"
	jasperhttp "github.com/jbrekelmans/go-lib/http"

	"github.com/go-mod-proxy/go/internal/pkg/service/auth"
)

// Authenticator authenticates GCE instance identity tokens.
type Authenticator struct {
	computeInstanceIdentityVerifier *jaspercompute.InstanceIdentityVerifier
	identityStore                   auth.IdentityStore
}

// NewAuthenticator creates a *Authenticator who's Authenticate decodes and verifies a GCE instance identity JWT token
// and returns the *auth.Identity with the GCE instance identity (by doing a lookup into identityStore).
func NewAuthenticator(
	computeInstanceIdentityVerifier *jaspercompute.InstanceIdentityVerifier,
	identityStore auth.IdentityStore) (*Authenticator, error) {
	if computeInstanceIdentityVerifier == nil {
		return nil, fmt.Errorf("computeInstanceIdentityVerifier must not be nil")
	}
	if identityStore == nil {
		return nil, fmt.Errorf("identityStore must not be nil")
	}
	a := &Authenticator{
		computeInstanceIdentityVerifier: computeInstanceIdentityVerifier,
		identityStore:                   identityStore,
	}
	return a, nil
}

// Authenticate decodes and verifies a GCE instance identity JWT token and
// either returns a non-nil *auth.Identity (first return parameter) or a non-nil error (second return parameter).
// The *auth.Identity is looked up from the auth.IdentityStore passed to NewAuthenticator.
func (a *Authenticator) Authenticate(ctx context.Context, bearerToken string) (interface{}, error) {
	instanceIdentity, err := a.computeInstanceIdentityVerifier.Verify(ctx, bearerToken)
	if err != nil {
		if _, ok := err.(*jaspercompute.VerifyError); ok {
			return nil, jasperhttp.ErrorInvalidBearerToken(fmt.Sprintf("invalid token: %v", err))
		}
		return nil, err
	}
	identity, err := a.identityStore.FindByGCEInstanceIdentityBindingEmail(instanceIdentity.Claims2.Email)
	if err != nil {
		if err == auth.ErrNotFound {
			return nil, jasperhttp.ErrorInvalidBearerToken(fmt.Sprintf("no identity exists that is bound to the GCE instance identity %s", instanceIdentity.Claims2.Email))
		}
		return nil, err
	}
	return identity, nil
}
