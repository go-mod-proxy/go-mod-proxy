package config

import (
	"context"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"
	"google.golang.org/api/iam/v1"
)

type validatorUsingGoogle struct {
	cfg        *Config
	ctx        context.Context
	emails     map[string]struct{}
	errors     *errorBag
	iamService *iam.Service
	waitGroup  sync.WaitGroup
}

func NewValidatorUsingGoogle(ctx context.Context, cfg *Config, iamService *iam.Service) (*validatorUsingGoogle, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	if cfg == nil {
		return nil, fmt.Errorf("cfg must not be nil")
	}
	if iamService == nil {
		return nil, fmt.Errorf("iamService must not be nil")
	}
	return &validatorUsingGoogle{
		cfg:        cfg,
		ctx:        ctx,
		emails:     map[string]struct{}{},
		errors:     newErrorBag(),
		iamService: iamService,
	}, nil
}

func (v *validatorUsingGoogle) Run() error {
	vctx := &validateValueContext{
		errorBag: v.errors,
	}
	vctxIdentities := vctx.Child("clientAuth").Child("identities")
	for i, identity := range v.cfg.ClientAuth.Identities {
		vctxIdentity := vctxIdentities.Child(i)
		if identity.GCEInstanceIdentityBinding != nil {
			v.validateGCEInstanceIdentityBinding(vctxIdentity, identity.GCEInstanceIdentityBinding)
		}
	}
	v.waitGroup.Wait()
	return v.errors.Err()
}

func (v *validatorUsingGoogle) validateGCEInstanceIdentityBinding(vctx *validateValueContext, b *GCEInstanceIdentityBinding) {
	// Test that we have permissions to validate instance identity tokens from GCE instances with the service account
	// with email b.Email.
	// This also validates b.Email, without implementing any email address validation rules that may accidentally be stricter
	// than Google IAM.
	vctxEmail := vctx.Child("email")
	v.waitGroup.Add(1)
	go func() {
		defer v.waitGroup.Done()
		name := fmt.Sprintf("projects/-/serviceAccounts/%s", b.Email)
		log.Tracef("IAM: getting service account (name = %#v)", name)
		serviceAccount, err := v.iamService.Projects.ServiceAccounts.Get(name).Context(v.ctx).Do()
		if err != nil {
			vctxEmail.AddErrorf("value could not be validated: an unexpected error occurred, the server is missing IAM permissions, the service account %#v "+
				"does not exist, or the service account is not a user-managed service account: %v", b.Email, err)
		} else if serviceAccount.Email != b.Email {
			vctxEmail.AddError(`value is not a valid email or a canonical email`)
		} else if _, ok := v.emails[b.Email]; ok {
			vctxEmail.AddErrorf(`two identities are illegaly bound to the same GCE instance identity email %#v`, b.Email)
		} else {
			v.emails[b.Email] = struct{}{}
		}
	}()
}
