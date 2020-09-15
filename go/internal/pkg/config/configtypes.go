package config

import (
	"crypto/rsa"
	"fmt"
	"net/url"
	"strings"
	"time"

	internalhttpproxy "github.com/jbrekelmans/go-module-proxy/internal/pkg/httpproxy"
)

type Access int

const (
	AccessAllow Access = iota
	AccessDeny
)

func (a *Access) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	switch strings.ToLower(s) {
	case "allow":
		*a = AccessAllow
	case "deny":
		*a = AccessDeny
	default:
		return fmt.Errorf(`value must be a string case-insensitive equal to "allow" or "deny"`)
	}
	return nil
}

type AccessControlListElement struct {
	Access       Access   `yaml:"access"`
	Identities   []string `yaml:"identities"`
	ModuleRegexp Regexp   `yaml:"moduleRegexp"`
}

type AccessTokenAuthenticator struct {
	Audience   string        `yaml:"audience"`
	Secret     string        `yaml:"secret"`
	TimeToLive time.Duration `yaml:"timeToLive"`
}

type ClientAuth struct {
	AccessControlList []*AccessControlListElement `yaml:"acl"`
	Authenticators    *struct {
		AccessToken         *AccessTokenAuthenticator         `yaml:"accessToken"`
		GCEInstanceIdentity *GCEInstanceIdentityAuthenticator `yaml:"gceInstanceIdentity"`
	} `yaml:"authenticators"`
	Enabled    bool        `yaml:"enabled"`
	Identities []*Identity `yaml:"identities"`
}

type Config struct {
	ClientAuth         ClientAuth               `yaml:"clientAuth"`
	GitHub             []*GitHubInstance        `yaml:"gitHub"`
	HTTPProxy          *HTTPProxy               `yaml:"httpProxy"`
	MaxChildProcesses  int                      `yaml:"maxChildProcesses"`
	ModuleRewriteRules []*ModuleRewriteRule     `yaml:"moduleRewriteRules"`
	ParentProxy        *ParentProxy             `yaml:"parentProxy"`
	PrivateModules     []*PrivateModulesElement `yaml:"privateModules"`
	Storage            *Storage                 `yaml:"storage"`
}

type GCEInstanceIdentityAuthenticator struct {
	Audience string `yaml:"audience"`
}

type GCEInstanceIdentityBinding struct {
	Email string `yaml:"email"`
}

type GitHubApp struct {
	ID               int64           `yaml:"id"`
	PrivateKey       string          `yaml:"privateKey"`
	PrivateKeyParsed *rsa.PrivateKey `yaml:"-"`
}

type GitHubInstance struct {
	GitHubApps []*GitHubApp `yaml:"gitHubApps"`
	Host       string       `yaml:"host"`
	isValid    bool         `yaml:"-"`
}

type HTTPProxy struct {
	isValid       bool                       `yaml:"-"`
	NoProxy       string                     `yaml:"noProxy"`
	NoProxyParsed *internalhttpproxy.NoProxy `yaml:"-"`
	URL           string                     `yaml:"url"`
	URLParsed     *url.URL                   `yaml:"-"`
	User          string                     `yaml:"user"`
	Password      string                     `yaml:"password"`
}

type Identity struct {
	Name                       string                      `yaml:"name"`
	GCEInstanceIdentityBinding *GCEInstanceIdentityBinding `yaml:"gceInstanceIdentityBinding"`
	Password                   *string                     `yaml:"password"`
}

type ModuleRewriteRule struct {
	Regexp      Regexp `yaml:"regexp"`
	Replacement string `yaml:"replacement"`
}

type ParentProxy struct {
	URL       string   `yaml:"url"`
	URLParsed *url.URL `yaml:"-"`
}

type PrivateModulesElement struct {
	Auth           PrivateModulesElementAuth `yaml:"auth"`
	isValid        bool                      `yaml:"-"`
	PathPrefix     string                    `yaml:"pathPrefix"`
	PathPrefixHost string                    `yaml:"-"`
}

type PrivateModulesElementAuth struct {
	GitHubApp *int64 `yaml:"gitHubApp"`
}

type Storage struct {
	GCS *GCSStorage `yaml:"gcs"`
}

type GCSStorage struct {
	Bucket string `yaml:"bucket"`
}
