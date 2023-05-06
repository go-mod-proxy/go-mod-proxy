package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	internalhttpproxy "github.com/go-mod-proxy/go-mod-proxy/internal/httpproxy"
)

type HTTPProxyInfo struct {
	ProxyFunc         func(*url.URL) (*url.URL, error)
	LibcurlHTTPSProxy string
	LibcurlNoProxy    string
}

func GetHTTPProxyInfoAndUnsetEnviron(cfg *Config) (*HTTPProxyInfo, error) {
	var proxyURL *url.URL
	var proxyURLSource string
	if cfg != nil && cfg.HTTPProxy != nil {
		proxyURL = cfg.HTTPProxy.URLParsed
		proxyURLSource = "configuration file"
	}
	var errorSlice []string
	keys := []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"}
	for i, key := range keys {
		value, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		if value == "" {
			log.Warningf("ignoring environment variable %s because its value is empty", key)
		} else {
			proxyURLAlt, err := internalhttpproxy.ValidateProxyURL(value)
			if err != nil {
				if proxyURLSource == "" {
					// If this environment variable has precedence, return the error instead of logging it
					errorSlice = append(errorSlice, fmt.Sprintf("value of environment variable %s is not a valid URL: %v", key, err))
				} else {
					log.Errorf("value of environment variable %s is not a valid URL: %v", key, err)
				}
			} else if proxyURLAlt.User != nil {
				_, hasPwd := proxyURLAlt.User.Password()
				if !hasPwd {
					if proxyURLSource == "" {
						// git will try to use the git credental helper when setting user but not password, avoid this
						errorSlice = append(errorSlice, fmt.Sprintf(`value of environment variable %s is a URL with user information, but it `+
							`illegally does not have a password`, key))
					} else {
						log.Errorf(`value of environment variable %s is a URL with user information, but it illegally does not have a password`, key)
					}
				}
			}
			if i > 1 {
				errorSlice = append(errorSlice, fmt.Sprintf(`environment variable %s is not supported (did you mean to set environment `+
					`variable %s instead?)`, key, keys[i-2]))
			}
			if proxyURLSource == "" {
				proxyURL = proxyURLAlt
				proxyURLSource = fmt.Sprintf("environment variable %s", key)
			}
		}
		if err := os.Unsetenv(key); err != nil {
			errorSlice = append(errorSlice, fmt.Sprintf("unexpected error unsetting environment variable %s: %v", key, err))
		} else {
			log.Infof("unset environment variable %s", key)
		}
	}
	var noProxy *internalhttpproxy.NoProxy
	var noProxySource string
	var libcurlNoProxy string
	if cfg != nil && cfg.HTTPProxy != nil && cfg.HTTPProxy.NoProxyParsed != nil {
		noProxy = cfg.HTTPProxy.NoProxyParsed
		noProxySource = "configuration file"
		var err error
		libcurlNoProxy, err = noProxy.FormatLibcurlCompatible(true)
		if err != nil {
			errorSlice = append(errorSlice, fmt.Sprintf("%s is invalid: HTTP forward proxy bypass is invalid: %v", noProxySource, err))
		}
	}
	keys = []string{"NO_PROXY", "no_proxy"}
	for _, key := range keys {
		value, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		if value == "" {
			log.Warningf("ignoring environment variable %s because its value is empty", key)
		} else {
			var libcurlNoProxyAlt string
			noProxyAlt, err := internalhttpproxy.ParseNoProxy(value)
			if err != nil {
				if noProxySource == "" {
					// If this environment variable has precedence, return the error instead of logging it
					errorSlice = append(errorSlice, fmt.Sprintf("value of environment variable %s is not a valid URL: %v", key, err))
				} else {
					log.Errorf("value of environment variable %s is invalid: %v", key, err)
				}
			} else {
				libcurlNoProxyAlt, err = noProxyAlt.FormatLibcurlCompatible(true)
				if err != nil {
					errorSlice = append(errorSlice, fmt.Sprintf("value of environment variable %s is invalid: %v", key, err))
				}
			}
			if noProxySource == "" {
				noProxy = noProxyAlt
				noProxySource = fmt.Sprintf("environment variable %s", key)
				libcurlNoProxy = libcurlNoProxyAlt
			}
		}
		if err := os.Unsetenv(key); err != nil {
			errorSlice = append(errorSlice, fmt.Sprintf("unexpected error unsetting environment variable %s: %v", key, err))
		} else {
			log.Infof("unset environment variable %s", key)
		}
	}
	if proxyURLSource == "" {
		if noProxySource != "" {
			errorSlice = append(errorSlice, fmt.Sprintf(`neither the configuration sets a HTTP proxy URL nor is one of the environment `+
				`variables HTTPS_PROXY and HTTP_PROXY set`))
		}
	} else {
		log.Infof("effective source of HTTP forward proxy URL: %s", proxyURLSource)
	}
	if len(errorSlice) > 0 {
		return nil, errors.New(fmt.Sprintf("got %d error(s):\n  - ", len(errorSlice)) + strings.Join(errorSlice, "\n  - "))
	}
	if noProxySource == "" {
		noProxy = &internalhttpproxy.NoProxy{}
		libcurlNoProxy = "*"
		log.Infof(`no HTTP forward proxy bypass is configured`)
	} else {
		log.Infof(`effective source of HTTP forward proxy bypass ("NO_PROXY"): %s`, noProxySource)
	}
	h := &HTTPProxyInfo{
		LibcurlNoProxy: libcurlNoProxy,
	}
	if proxyURL != nil {
		h.LibcurlHTTPSProxy = proxyURL.String()
		var err error
		h.ProxyFunc, err = internalhttpproxy.ProxyFunc(noProxy, h.LibcurlHTTPSProxy)
		if err != nil {
			return nil, err
		}
	}
	return h, nil
}
