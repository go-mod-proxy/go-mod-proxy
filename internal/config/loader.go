package config

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	jasperurl "github.com/jbrekelmans/go-url"
	"gopkg.in/yaml.v2"

	internalhttpproxy "github.com/go-mod-proxy/go-mod-proxy/internal/httpproxy"
	"github.com/go-mod-proxy/go-mod-proxy/internal/util"
)

// LoadFromYAMLFile loads configuration from a YAML file.
func LoadFromYAMLFile(file string) (*Config, error) {
	fd, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	dir := filepath.Dir(file)
	if dir == file {
		return nil, fmt.Errorf("invalid file")
	}
	loader, err := NewLoader(fd, dir)
	if err != nil {
		return nil, err
	}
	cfg, err := loader.Run()
	if err != nil {
		return nil, fmt.Errorf("error loading %#v: %w", file, err)
	}
	return cfg, nil
}

// Loader is a helper type to split up configuration loading into multiple functions.
type Loader struct {
	cfg            *Config
	dir            string
	identityByName map[string]*Identity
	errors         *errorBag
	reader         io.Reader
}

// NewLoader encapsulates correct initialization of Loader.
func NewLoader(reader io.Reader, dir string) (*Loader, error) {
	if reader == nil {
		return nil, fmt.Errorf("reader must not be nil")
	}
	l := &Loader{
		cfg:            &Config{},
		dir:            dir,
		errors:         newErrorBag(),
		identityByName: map[string]*Identity{},
		reader:         reader,
	}
	return l, nil
}

func (l *Loader) resolveFile(file string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(l.dir, file)
}

// Run loads the configuration.
func (l *Loader) Run() (*Config, error) {
	if l.identityByName == nil {
		return nil, fmt.Errorf("l must be created via NewLoader")
	}
	decoder := yaml.NewDecoder(l.reader)
	decoder.SetStrict(true)
	if err := decoder.Decode(l.cfg); err != nil {
		return nil, err
	}
	l.validateConfig(&validateValueContext{
		errorBag: l.errors,
	}, l.cfg)
	if err := l.errors.Err(); err != nil {
		return nil, err
	}
	return l.cfg, nil
}

func (l *Loader) validateConfig(vctx *validateValueContext, cfg *Config) {
	vctxClientAuth := vctx.Child("clientAuth")
	vctxIdentities := vctxClientAuth.Child("identities")
	for i, identity := range cfg.ClientAuth.Identities {
		vctxIdentity := vctxIdentities.Child(i)
		if vctxIdentity.RequiredString(identity.Name) {
			if strings.ContainsAny(identity.Name, ":") {
				// To support encoding in Basic auth header
				vctxIdentity.Child("name").AddError(`value contains illegal character ":"`)
			} else {
				if _, ok := l.identityByName[identity.Name]; ok {
					vctxIdentities.AddErrorf(`two elements illegally have the same .name %#v`, identity.Name)
				} else {
					l.identityByName[identity.Name] = identity
				}
			}
		}
		if identity.Password != nil {
			l.validateSecret(vctxIdentity.Child("password"), identity.Password)
			if identity.Password.isValid && len(identity.Password.Plaintext) == 0 {
				vctxIdentity.Child("password").AddError("effective value of secret must not be empty")
			}
		}
		if b := identity.GCEInstanceIdentityBinding; b != nil {
			vctxIdentity.Child("gceInstanceIdentityBinding").RequiredString(b.Email)
		}
	}
	vctxAccessControlList := vctxClientAuth.Child("accessControlList")
	for i, aclElem := range cfg.ClientAuth.AccessControlList {
		if aclElem == nil {
			vctxAccessControlList.Child(i).AddRequiredError()
		} else {
			l.validateAccessControlListElement(vctxAccessControlList.Child(i), aclElem)
		}
	}

	gitHubInstanceIndex := map[string]int{}
	for i, gitHubInstance := range cfg.GitHub {
		if gitHubInstance == nil {
			vctx.Child("gitHub").Child(i).AddRequiredError()
		} else {
			l.validateGitHubInstance(vctx.Child("gitHub").Child(i), gitHubInstance)
			if gitHubInstance.isValid {
				if state := gitHubInstanceIndex[gitHubInstance.Host]; state == 0 {
					gitHubInstanceIndex[gitHubInstance.Host] = i + 1
				} else if state > 0 {
					vctx.Child("gitHubApps").AddErrorf("no two elements must have the same .host but [%d] and [%d] have .host %#v", i,
						state-1, gitHubInstance.Host)
					gitHubInstanceIndex[gitHubInstance.Host] = -1
				}
			}
		}
	}

	if cfg.HTTPProxy != nil {
		l.validateHTTPProxy(vctx.Child("httpProxy"), cfg.HTTPProxy)
	}
	if cfg.MaxChildProcesses == 0 {
		vctx.Child("maxChildProcesses").AddErrorf("value must be set (to a positive integer))")
	} else if cfg.MaxChildProcesses < 0 {
		vctx.Child("maxChildProcesses").AddErrorf("value must not be negative")
	}
	l.validateParentProxy(vctx.Child("parentProxy"), &l.cfg.ParentProxy)
	l.validatePrivateModules(vctx.Child("privateModules"), l.cfg.PrivateModules)
	for i, privateModulesElement := range l.cfg.PrivateModules {
		if privateModulesElement != nil && privateModulesElement.isValid {
			if privateModulesElement.Auth.GitHubApp != nil {
				j := gitHubInstanceIndex[privateModulesElement.PathPrefixHost]
				if j == 0 {
					vctx.AddErrorf(`.privateModules[%d].auth.gitHubApp != null but .gitHub has no element with .host equal to `+
						`the host of .privateModules[%d].pathPrefix (%#v)`, i, i, privateModulesElement.PathPrefixHost)
				} else if j > 0 {
					gitHubInstance := l.cfg.GitHub[j-1]
					if gitHubInstance.isValid {
						found := false
						for _, gitHubApp := range gitHubInstance.GitHubApps {
							if gitHubApp.ID == *privateModulesElement.Auth.GitHubApp {
								found = true
								break
							}
						}
						if !found {
							vctx.AddErrorf(`.privateModules[%d] configures GitHub App based authentication on host %#v `+
								`and .gitHub[%d] has .host %#v but .gitHub[%d].gitHubApps has no element with .id equal to `+
								`.privateModules[%d].auth.gitHubApp = %d`, i, privateModulesElement.PathPrefixHost, j-1,
								privateModulesElement.PathPrefixHost, j-1, i, *privateModulesElement.Auth.GitHubApp)
						}
					}
				}
			}
		}
	}
	if l.cfg.PublicModules.SumDatabase != nil {
		l.validateSumDatabaseElement(vctx.Child("publicModules").Child("sumDatabase"), l.cfg.PublicModules.SumDatabase)
	}
	if l.cfg.Storage == nil {
		vctx.Child("storage").AddRequiredError()
	} else {
		l.validateStorage(vctx.Child("storage"), l.cfg.Storage)
	}
	if l.cfg.SumDatabaseProxy == nil {
		vctx.Child("sumDatabaseProxy").AddRequiredError()
	} else {
		l.validateSumDatabaseProxy(vctx.Child("sumDatabaseProxy"), l.cfg.SumDatabaseProxy)
	}
}

func (l *Loader) validateAccessControlListElement(vctx *validateValueContext, aclElem *AccessControlListElement) {
	if len(aclElem.Identities) > 0 {
		vctxIdentities := vctx.Child("identities")
		unique := map[string]struct{}{}
		for i, name := range aclElem.Identities {
			_, ok := unique[name]
			if ok {
				vctxIdentities.AddErrorf("two elements illegaly are the same (%#v)", name)
			} else {
				unique[name] = struct{}{}
				identity := l.identityByName[name]
				if identity == nil {
					vctxIdentities.Child(i).AddErrorf(`value (%#v) names an identity that has not been defined in .security.identities`, name)
				}
			}
		}
	}
}

func (l *Loader) validateGCSStorage(vctx *validateValueContext, gcs *GCSStorage) {
	if gcs.Bucket == "" {
		vctx.AddError(".bucket must not be empty")
	}
}

func (l *Loader) validateGitHubInstance(vctx *validateValueContext, gitHubInstance *GitHubInstance) {
	n := vctx.ErrorCount()
	vctx.RequiredString(gitHubInstance.Host)
	appIndex := map[int64]int{}
	for i, app := range gitHubInstance.GitHubApps {
		vctxApp := vctx.Child("gitHubApps").Child(i)
		if app.ID == 0 {
			vctxApp.Child("id").AddError("value must be set (to a positive integer)")
		} else if app.ID < 0 {
			vctxApp.Child("id").AddError("value must not be negative")
		} else {
			vctxAppPrivateKey := vctxApp.Child("privateKey")
			if app.PrivateKey == nil {
				vctxAppPrivateKey.AddRequiredError()
			} else {
				l.validateSecret(vctxAppPrivateKey, app.PrivateKey)
				if app.PrivateKey.isValid {
					app.PrivateKeyParsed = l.validateRSAPrivateKey(vctxAppPrivateKey, app.PrivateKey.Plaintext)
				}
			}
			if state := appIndex[app.ID]; state == 0 {
				appIndex[app.ID] = i + 1
			} else if state > 0 {
				vctx.Child("gitHubApps").AddErrorf("no two elements must have the same .id but [%d] and [%d] have .id %d", i,
					state-1, app.ID)
				appIndex[app.ID] = -1
			}
		}
	}
	gitHubInstance.isValid = n == vctx.ErrorCount()
}

func (l *Loader) validateHTTPProxy(vctx *validateValueContext, httpProxy *HTTPProxy) {
	n := l.errors.ErrorCount()
	if vctx.Child("url").RequiredString(httpProxy.URL) {
		var err error
		httpProxy.URLParsed, err = internalhttpproxy.ValidateProxyURL(httpProxy.URL)
		if err != nil {
			vctx.Child("url").AddErrorf("value is not a valid URL: %v", err)
		} else if httpProxy.URLParsed.User != nil {
			vctx.Child("url").AddError("URL must not have user information")
		}
	}
	var err error
	httpProxy.NoProxyParsed, err = internalhttpproxy.ParseNoProxy(httpProxy.NoProxy)
	if err != nil {
		vctx.Child("noProxy").AddErrorf("%v", err)
	}
	user := httpProxy.User
	if user != "" {
		if strings.ContainsAny(user, "\x00:") {
			vctx.Child("user").AddError(`value contains illegal zero byte or illegal character ":"`)
		}
	}

	var password string
	var hasPassword bool
	if httpProxy.Password != nil {
		hasPassword = true
		l.validateSecret(vctx.Child("password"), httpProxy.Password)
		if httpProxy.Password.isValid {
			if bytes.ContainsAny(httpProxy.Password.Plaintext, "\x00") {
				vctx.Child("password").AddError(`effective secret value contains illegal zero byte`)
			}
		}
	}
	if (user != "") != hasPassword {
		// git will try to use the git credental helper when setting user but not password, avoid this
		vctx.AddError(`either .user must be set (to a non-empty string) and .password must both be set (to a non-null value) or neither`)
	}
	httpProxy.isValid = l.errors.ErrorCount() == n
	if httpProxy.isValid && hasPassword {
		httpProxy.URLParsed.User = url.UserPassword(user, password)
	}
}

func (l *Loader) validateParentProxy(vctx *validateValueContext, parentProxy *ParentProxy) {
	var err error
	parentProxy.URLParsed, err = jasperurl.ValidateURL(parentProxy.URL, jasperurl.ValidateURLOptions{
		Abs:                                      jasperurl.NewBool(true),
		AllowedSchemes:                           []string{"https"},
		NormalizePort:                            new(bool),
		StripFragment:                            true,
		StripQuery:                               true,
		StripPathTrailingSlashes:                 true,
		StripPathTrailingSlashesNoPercentEncoded: true,
		User:                                     new(bool),
	})
	if err != nil {
		vctx.Child("url").AddErrorf("value is not a valid URL: %v", err)
	}
}

func (l *Loader) validatePrivateModules(vctx *validateValueContext, privateModules []*PrivateModulesElement) {
	pathPrefixes := map[string]int{}
	for i, privateModulesElement := range privateModules {
		if privateModulesElement == nil {
			vctx.Child(i).AddRequiredError()
		} else {
			l.validatePrivateModulesElement(vctx.Child(i), privateModulesElement)
			if privateModulesElement.isValid {
				for pathPrefix, j := range pathPrefixes {
					if util.PathIsLexicalDescendant(pathPrefix, privateModulesElement.PathPrefix) ||
						util.PathIsLexicalDescendant(privateModulesElement.PathPrefix, pathPrefix) {
						vctx.AddErrorf(`no two elements can have path prefixes that match overlapping sets of paths but `+
							`the sets of paths matched by [%d].pathPrefix (%#v) and [%d].pathPrefix (%#v) overlap`, j,
							pathPrefix, i, privateModulesElement.PathPrefix)
					}
				}
				pathPrefixes[privateModulesElement.PathPrefix] = i
			}
		}
	}
}

func (l *Loader) validatePrivateModulesElement(vctx *validateValueContext, privateModulesElement *PrivateModulesElement) {
	n := vctx.ErrorCount()
	if privateModulesElement.Auth.GitHubApp == nil {
		vctx.Child("auth").AddError(".gitHubApp must be set (to a non-null value)")
	}
	if privateModulesElement.PathPrefix == "" {
		vctx.Child("pathPrefix").AddError("value must be set (to a non-empty string)")
	} else if privateModulesElement.PathPrefix[len(privateModulesElement.PathPrefix)-1] == '/' {
		vctx.Child("pathPrefix").AddError(`value must not end with a "/"`)
	} else {
		var host string
		i := strings.IndexByte(privateModulesElement.PathPrefix, '/')
		if i >= 0 {
			host = privateModulesElement.PathPrefix[:i]
		} else {
			host = privateModulesElement.PathPrefix
		}
		privateModulesElement.PathPrefixHost = host
	}
	privateModulesElement.isValid = n == vctx.ErrorCount()
}

func (l *Loader) validateRSAPrivateKey(vctx *validateValueContext, bytes []byte) *rsa.PrivateKey {
	var firstKey *rsa.PrivateKey
	i := 0
	for {
		var block *pem.Block
		block, bytes = pem.Decode(bytes)
		if block == nil {
			break
		}
		if block.Type == "PRIVATE KEY" || strings.HasSuffix(block.Type, " PRIVATE KEY") {
			key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				keyInterf, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
				if err2 != nil {
					vctx.AddErrorf("effective secret value's %s PEM block (type %s) could neither be parsed using x509.ParsePKCS1PrivateKey nor x509.ParsePKCS8PrivateKey",
						util.FormatIth(i), block.Type)
					return nil
				}
				var ok bool
				key, ok = keyInterf.(*rsa.PrivateKey)
				if !ok {
					vctx.AddErrorf("effective secret value's %s PEM block (type %s)'s data was recognized as a private key but is not an RSA private key",
						util.FormatIth(i), block.Type)
					return nil
				}
			}
			if firstKey != nil {
				vctx.AddErrorf("effective secret value illegally has multiple PEM blocks (%s PEM block has type %s)", util.FormatIth(i), block.Type)
				return nil
			} else {
				firstKey = key
			}
		} else {
			vctx.AddErrorf("effective secret value's %s PEM block has an unexpected type %s", util.FormatIth(i), block.Type)
			return nil
		}
		i++
	}
	if i == 0 {
		vctx.AddError("effective secret value must have a private key PEM block but value has no valid PEM blocks")
		return nil
	}
	return firstKey
}

func (l *Loader) validateStorage(vctx *validateValueContext, storage *Storage) {
	x := 0
	if storage.FS != nil {
		x++
	}
	if storage.GCS != nil {
		x++
		l.validateGCSStorage(vctx.Child("gcs"), storage.GCS)
	}
	if x != 1 {
		vctx.AddError("exactly one of .fs and .gcs must be set (to a non-null value)")
	}
}

func (l *Loader) validateSecret(vctx *validateValueContext, secret *Secret) {
	n := vctx.ErrorCount()
	x := 0
	if secret.EnvVar != nil {
		x++
		if value, ok := os.LookupEnv(*secret.EnvVar); !ok {
			vctx.AddErrorf(`secret is sourced from an environment variable because .env is set to %#v but no environment variable `+
				`named %#v exists`, *secret.EnvVar, *secret.EnvVar)
		} else {
			secret.Plaintext = []byte(value)
		}
	}
	if secret.File != nil {
		x++
		file := l.resolveFile(*secret.File)
		var err error
		secret.Plaintext, err = os.ReadFile(file)
		if err != nil {
			vctx.AddErrorf(`secret is sourced from a file because .file is set to %#v but got unexpected error loading file %#v: %v`,
				*secret.File, file, err)
		}
	}
	if x != 1 {
		vctx.AddError("exactly one of .envVar and .file must be set (to a non-null value)")
	}
	secret.isValid = vctx.ErrorCount() == n
}

func (l *Loader) validateSumDatabaseElement(vctx *validateValueContext, sumDBElement *SumDatabaseElement) {
	n := vctx.ErrorCount()
	var err error
	sumDBElement.URLParsed, err = jasperurl.ValidateURL(sumDBElement.URL, jasperurl.ValidateURLOptions{
		Abs:                                      jasperurl.NewBool(true),
		AllowedSchemes:                           []string{"https"},
		NormalizePort:                            new(bool),
		StripFragment:                            true,
		StripQuery:                               true,
		StripPathTrailingSlashes:                 true,
		StripPathTrailingSlashesNoPercentEncoded: true,
		User:                                     new(bool),
	})
	if err != nil {
		vctx.Child("url").AddErrorf("value is not a valid URL: %v", err)
	}
	if vctx.Child("name").RequiredString(sumDBElement.Name) {
		if len(strings.Fields(sumDBElement.Name)) > 1 {
			vctx.Child("name").AddError("value contains illegal unicode white space")
		}
		if strings.ContainsAny(sumDBElement.Name, "+") {
			vctx.Child("name").AddError(`value contains illegal character "+"`)
		}
	}
	if vctx.Child("publicKey").RequiredString(sumDBElement.PublicKey) {
		if len(strings.Fields(sumDBElement.Name)) > 1 {
			vctx.Child("name").AddError("value contains illegal unicode white space")
		}
	}
	sumDBElement.isValid = n != vctx.ErrorCount()
}

func (l *Loader) validateSumDatabaseProxy(vctx *validateValueContext, sumDatabaseProxy *SumDatabaseProxy) {
	nameIndex := map[string]int{}
	vctxSumDatabases := vctx.Child("sumDatabases")
	for i, sumDBElement := range sumDatabaseProxy.SumDatabases {
		vctxSumDBElement := vctxSumDatabases.Child(i)
		if sumDBElement == nil {
			vctxSumDBElement.AddRequiredError()
		} else {
			l.validateSumDatabaseElement(vctxSumDBElement, sumDBElement)
			if sumDBElement.isValid {
				if state := nameIndex[sumDBElement.Name]; state == 0 {
					nameIndex[sumDBElement.Name] = i + 1
				} else if state > 0 {
					vctxSumDatabases.AddErrorf("no two elements must have the same .name but [%d] and [%d] have .name %#v", i,
						state-1, sumDBElement.Name)
					nameIndex[sumDBElement.Name] = -1
				}
			}
		}
	}
}
