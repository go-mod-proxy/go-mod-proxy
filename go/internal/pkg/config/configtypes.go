package config

import (
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	internalhttpproxy "github.com/go-mod-proxy/go/internal/pkg/httpproxy"
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
	Secret     *Secret       `yaml:"secret"`
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
	ClientAuth        ClientAuth               `yaml:"clientAuth"`
	GitHub            []*GitHubInstance        `yaml:"gitHub"`
	HTTPProxy         *HTTPProxy               `yaml:"httpProxy"`
	MaxChildProcesses int                      `yaml:"maxChildProcesses"`
	ParentProxy       ParentProxy              `yaml:"parentProxy"`
	PrivateModules    []*PrivateModulesElement `yaml:"privateModules"`
	PublicModules     PublicModules            `yaml:"publicModules"`
	Storage           *Storage                 `yaml:"storage"`
	SumDatabaseProxy  *SumDatabaseProxy        `yaml:"sumDatabaseProxy"`
	TLS               *TLS                     `yaml:"tls"`
}

type GCEInstanceIdentityAuthenticator struct {
	Audience string `yaml:"audience"`
}

type GCEInstanceIdentityBinding struct {
	Email string `yaml:"email"`
}

type GCSStorage struct {
	BypassHTTPProxy bool   `yaml:"bypassHTTPProxy"`
	Bucket          string `yaml:"bucket"`
}

type GitHubApp struct {
	ID               int64           `yaml:"id"`
	PrivateKey       *Secret         `yaml:"privateKey"`
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
	Password      *Secret                    `yaml:"password"`
}

type Identity struct {
	Name                       string                      `yaml:"name"`
	GCEInstanceIdentityBinding *GCEInstanceIdentityBinding `yaml:"gceInstanceIdentityBinding"`
	Password                   *Secret                     `yaml:"password"`
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

type PublicModules struct {
	SumDatabase *SumDatabaseElement `yaml:"sumDatabase"`
}

type Secret struct {
	EnvVar    *string `yaml:"envVar"`
	File      *string `yaml:"file"`
	isValid   bool    `yaml:"-"`
	Plaintext []byte  `yaml:"-"`
}

type Storage struct {
	GCS *GCSStorage `yaml:"gcs"`
}

type SumDatabaseElement struct {
	isValid   bool     `yaml:"-"`
	Name      string   `yaml:"name"`
	PublicKey string   `yaml:"publicKey"`
	URL       string   `yaml:"url"`
	URLParsed *url.URL `yaml:"-"`
}

// FormatGoSumDBEnvVar represents s as the value of a GOSUMDB environment variable as defined
// by the Go toolchain.
func (s *SumDatabaseElement) FormatGoSumDBEnvVar() string {
	return fmt.Sprintf("%s+%s %s", s.Name, s.PublicKey, s.URLParsed.String())
}

type SumDatabaseProxy struct {
	DiscourageClientDirectSumDatabaseConnections bool `yaml:"discourageClientDirectSumDatabaseConnections"`

	SumDatabases []*SumDatabaseElement `yaml:"sumDatabases"`
}

type TLS struct {
	MinVersion TLSVersion `yaml:"minVersion"`
}

type TLSVersion uint16

func (t *TLSVersion) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	switch strings.ToUpper(s) {
	case "TLS1.0":
		*t = tls.VersionTLS10
	case "TLS1.1":
		*t = tls.VersionTLS11
	case "TLS1.2":
		*t = tls.VersionTLS12
	case "TLS1.3":
		*t = tls.VersionTLS13
	default:
		return fmt.Errorf(`value must be a string case-insensitive equal to one of "TLS1.0", "TLS1.1", "TLS1.2" and "TLS1.3"`)
	}
	return nil
}
