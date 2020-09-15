package auth

import (
	"errors"
	"fmt"

	"github.com/jbrekelmans/go-module-proxy/internal/pkg/config"
)

var ErrNotFound = errors.New("not found")

type Identity = config.Identity

type IdentityStore interface {
	Add(identity *Identity) error
	FindByGCEInstanceIdentityBindingEmail(email string) (*Identity, error)
	FindByName(name string) (*Identity, error)
}

type identityStore struct {
	byGCEInstanceIdentityBindingEmail map[string]*Identity
	byName                            map[string]*Identity
}

func NewInMemoryIdentityStore() (IdentityStore, error) {
	i := &identityStore{
		byGCEInstanceIdentityBindingEmail: map[string]*Identity{},
		byName:                            map[string]*Identity{},
	}
	return i, nil
}

// Add adds an identity to the identity store. identity is assumed to never be modified after being passed to
// add.
func (i *identityStore) Add(identity *Identity) error {
	if identity == nil {
		return fmt.Errorf("identity must not be nil")
	}
	if identity.Name == "" {
		return fmt.Errorf("identity.Name must not be empty")
	}
	_, ok := i.byName[identity.Name]
	if ok {
		return fmt.Errorf("cannot add identity named %#v because otherwise two different identities would have the same name", identity.Name)
	}
	if identity.GCEInstanceIdentityBinding != nil {
		if identity.GCEInstanceIdentityBinding.Email == "" {
			return fmt.Errorf("identity.GCEInstanceIdentityBinding.Email must not be  empty")
		}
		_, ok := i.byGCEInstanceIdentityBindingEmail[identity.GCEInstanceIdentityBinding.Email]
		if ok {
			return fmt.Errorf("cannot add the identity named %#v because otherwise two different identities would be bound to the "+
				"same GCE instance identity email (%#v)", identity.Name, identity.GCEInstanceIdentityBinding.Email)
		}
		i.byGCEInstanceIdentityBindingEmail[identity.GCEInstanceIdentityBinding.Email] = identity
	}
	i.byName[identity.Name] = identity
	return nil
}

func (i *identityStore) FindByGCEInstanceIdentityBindingEmail(email string) (*Identity, error) {
	identity, ok := i.byGCEInstanceIdentityBindingEmail[email]
	if ok {
		return identity, nil
	}
	return nil, ErrNotFound
}

func (i *identityStore) FindByName(name string) (*Identity, error) {
	identity, ok := i.byName[name]
	if ok {
		return identity, nil
	}
	return nil, ErrNotFound
}
