package httpproxy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	jasperurl "github.com/jbrekelmans/go-lib/url"

	"github.com/go-mod-proxy/go-mod-proxy/internal/util"
)

type domainPort struct {
	domain              string
	matchSubdomainsOnly bool
	port                int32
}

type ipPort struct {
	ip   net.IP
	port int32
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

type NoProxy struct {
	cidrs      []*net.IPNet
	domains    []domainPort
	ips        []ipPort
	isAsterisk bool
}

// ParseNoProxy parses values of NO_PROXY environment variables similar to how "http/httpproxy".FromEnvironment
// parses them, with the following differences:
//  1. If the port of a host is empty (i.e. "example.com:") then an error is returned.
//  2. If the port of a host is a named port (i.e. "example.com:http") then an error is returned.
//  3. If the port of a host is not positive (i.e. "zeronotallowed.example.com:0") then an error is returned.
//  4. If the hostname of a host has non-ASCII characters then an error is returned. A hostname must be normalized as per
//     "golang.org/x/net/idna".Lookup.ToASCII (otherwise it never matches anything). NOTE: validation of hostnames can be stricter, however
//     this level of strictness matches the "http/httpproxy" and "http" Golang packages (despite lower level code potentially being
//     stricter).
//
// This function always returns a non-nil *NoProxy and any error returned is suitable for printing in logs, but the error's Error() may
// return a string with line feeds. As such, this function is suitable for validation of the NO_PROXY environment variable.
func ParseNoProxy(noProxyStr string) (*NoProxy, error) {
	var errorSlice []string
	n := &NoProxy{}
	for i, elem := range strings.Split(noProxyStr, ",") {
		elemNorm := strings.TrimSpace(elem)
		if elemNorm == "" {
			continue
		}
		elemNorm = strings.ToLower(elemNorm)
		if elemNorm == "*" {
			n.isAsterisk = true
			continue
		}
		_, cidr, cidrErr := net.ParseCIDR(elemNorm)
		if cidrErr == nil {
			n.cidrs = append(n.cidrs, cidr)
			continue
		}
		var portInt32 int32
		hostname, portStr, hostnamePortErr := net.SplitHostPort(elemNorm)
		if hostnamePortErr == nil {
			if hostname == "" {
				errorSlice = append(errorSlice, fmt.Sprintf("%s comma-delimited element (%#v) is a host but the hostname "+
					" consists of only unicode white space", util.FormatIth(i+1), elem))
				continue
			}
			if hostname[0] == '[' && hostname[len(hostname)-1] == ']' {
				hostname = hostname[1 : len(hostname)-1]
			}
			if hostname == "" {
				errorSlice = append(errorSlice, fmt.Sprintf(`%s comma-delimited element (%#v) is a host but the hostname (%#v, after `+
					`trimming unicode white space) is invalid`, util.FormatIth(i+1), elem, `[]`))
				continue
			}
			portInt64, err := strconv.ParseInt(portStr, 10, 32)
			if err != nil {
				errorSlice = append(errorSlice, fmt.Sprintf(`%s comma-delimited element (%#v) is a host but got error parsing the port after `+
					`trimming unicode white space (%#v): %v`, util.FormatIth(i+1), elem, portStr, err))
				continue
			}
			if portInt64 <= 0 {
				errorSlice = append(errorSlice, fmt.Sprintf(`%s comma-delimited element (%#v) is a host but the port (%d) is illegally not `+
					`positive`, util.FormatIth(i+1), elem, portInt64))
				continue
			}
			portInt32 = int32(portInt64)
		} else {
			hostname = elemNorm
		}
		ip := net.ParseIP(hostname)
		if ip != nil {
			n.ips = append(n.ips, ipPort{
				ip:   ip,
				port: portInt32,
			})
			continue
		}
		if !isASCII(hostname) {
			if hostnamePortErr == nil {
				errorSlice = append(errorSlice, fmt.Sprintf(`%s comma-delimited element (%#v) is a hostname but illegally contains `+
					`non-ASCII non-unicode-whitespace characters (please use Go's "golang.org/x/net/idna".Lookup.ToASCII to normalize the hostname)`,
					util.FormatIth(i+1), elem))
				continue
			}
			errorSlice = append(errorSlice, fmt.Sprintf(`%s comma-delimited element (%#v) is a host but the hostname illegally contains `+
				`non-ASCII non-unicode-whitespace characters (please use Go's "golang.org/x/net/idna".Lookup.ToASCII to normalize the hostname)`,
				util.FormatIth(i+1), elem))
			continue
		}
		matchSubdomainsOnly := true
		if strings.HasPrefix(hostname, "*.") {
			hostname = hostname[1:]
		} else if hostname[0] != '.' {
			matchSubdomainsOnly = false
			hostname = "." + hostname
		}
		n.domains = append(n.domains, domainPort{
			domain:              hostname,
			matchSubdomainsOnly: matchSubdomainsOnly,
			port:                portInt32,
		})
	}
	if len(errorSlice) > 0 {
		return n, errors.New(fmt.Sprintf("got %d error(s):\n  - ", len(errorSlice)) + strings.Join(errorSlice, "\n  - "))
	}
	if n.isAsterisk {
		n.cidrs = nil
		n.domains = nil
		n.ips = nil
	}
	return n, nil
}

// FormatLibcurlCompatible formats the value of a NO_PROXY environment variable that is compatible with libcurl (see
// https://curl.haxx.se/libcurl/c/CURLOPT_NOPROXY.html). If the *NoProxy cannot be represented as such then an error is returned.
// For example, if the *NoProxy contains a CIDR or matches only specific ports of a hostname then an error is returned.
func (n *NoProxy) FormatLibcurlCompatible(addLoopbackHostnames bool) (string, error) {
	if n.isAsterisk {
		return "*", nil
	}
	var errorSlice []string
	for _, cidr := range n.cidrs {
		errorSlice = append(errorSlice, fmt.Sprintf(`converting CIDR %s to a representation compatible with libcurl is not implemented`,
			cidr.String()))
	}
	var sb strings.Builder
	for _, domainPort := range n.domains {
		var x string
		if domainPort.matchSubdomainsOnly {
			x = domainPort.domain
			if domainPort.port != 0 {
				errorSlice = append(errorSlice, fmt.Sprintf(`matching port %d (of subdomains of %#v) is not possible in libcurl`,
					domainPort.port, x))
			}
			if addLoopbackHostnames && x == ".localhost" {
				continue
			}
		} else {
			x = domainPort.domain[1:]
			if domainPort.port != 0 {
				errorSlice = append(errorSlice, fmt.Sprintf(`matching port %d (of domainn %#v and its subdomains) is not possible in libcurl`,
					domainPort.port, x))
			}
			if addLoopbackHostnames && x == "localhost" {
				continue
			}
		}
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(x)
	}
	for _, ipPort := range n.ips {
		if ipPort.port != 0 {
			errorSlice = append(errorSlice, fmt.Sprintf(`matching port %d (of IP %s) is not possible in libcurl`, ipPort.port,
				ipPort.ip.String()))
		}
		if addLoopbackHostnames && ipPort.ip.IsLoopback() {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(ipPort.ip.String())
	}
	if len(errorSlice) > 0 {
		return "", errors.New(fmt.Sprintf("the HTTP forward proxy bypass is not representable in libcurl, %d reason(s):\n  - ",
			len(errorSlice)) + strings.Join(errorSlice, "\n  - "))
	}
	if addLoopbackHostnames {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		// In case libcurl special-cases the hostname "localhost" we also include ".localhost", in an attempt to make sure that hostnames
		// like "bla.localhost" also match the proxy bypass).
		sb.WriteString(".localhost,localhost,127.0.0.1,::1")
	}
	return sb.String(), nil
}

func (n *NoProxy) UseProxy(url1 *url.URL) (bool, error) {
	if !url1.IsAbs() {
		return false, nil
	}
	url2 := new(url.URL)
	*url2 = *url1
	err := jasperurl.NormalizePort(url2, true, nil)
	if err != nil {
		return false, fmt.Errorf("URL is invalid or has a non-integer port: %w", err)
	}
	hostname1 := url2.Hostname()
	hostname2, err := idnaASCII(hostname1)
	if err != nil {
		return false, fmt.Errorf(`URL is invalid: unexpected error doing "golang.org/x/net/idna".Lookup.ToASCII on host (%#v): %w`,
			hostname1, err)
	}
	port, err := strconv.ParseInt(url2.Port(), 10, 32)
	if err != nil {
		// This should never happen as jasperurl.NormalizePort errors if a port cannot be represented as int32.
		panic(err)
	}
	return n.useProxyHost(strings.ToLower(hostname2), int32(port)), nil
}

func (n *NoProxy) useProxyHost(hostname string, port int32) bool {
	if n.isAsterisk {
		return false
	}
	ip := net.ParseIP(hostname)
	if ip != nil {
		if ip.IsLoopback() {
			return false
		}
		for _, cidr := range n.cidrs {
			if cidr.Contains(ip) {
				return false
			}
		}
		for _, ipPort := range n.ips {
			if ip.Equal(ipPort.ip) && (ipPort.port == 0 || ipPort.port == port) {
				return false
			}
		}
	} else {
		if strings.HasSuffix(hostname, ".localhost") || hostname == "localhost" {
			return false
		}
		for _, domainPort := range n.domains {
			if strings.HasSuffix(hostname, domainPort.domain) || (!domainPort.matchSubdomainsOnly && domainPort.domain[1:] == hostname) {
				if domainPort.port == 0 || domainPort.port == port {
					return false
				}
			}
		}
	}
	return true
}

func ProxyFunc(n *NoProxy, httpsProxy string) (func(*url.URL) (*url.URL, error), error) {
	if n == nil {
		return nil, fmt.Errorf("n must not be nil")
	}
	httpsProxyParsed1, err := ValidateProxyURL(httpsProxy)
	if err != nil {
		return nil, fmt.Errorf("httpsProxy is not a valid URL: %w", err)
	}
	return func(url1 *url.URL) (*url.URL, error) {
		useProxy, err := n.UseProxy(url1)
		if err != nil {
			return nil, err
		}
		if useProxy {
			httpsProxyParsed2 := new(url.URL)
			*httpsProxyParsed2 = *httpsProxyParsed1
			return httpsProxyParsed2, nil
		}
		return nil, nil
	}, nil
}

func ValidateProxyURL(proxyURL string) (*url.URL, error) {
	u, err := jasperurl.ValidateURL(proxyURL, jasperurl.ValidateURLOptions{
		Abs:                                      jasperurl.NewBool(true),
		AllowedSchemes:                           []string{"http", "https"},
		StripFragment:                            true,
		StripQuery:                               true,
		StripPathTrailingSlashes:                 true,
		StripPathTrailingSlashesNoPercentEncoded: true,
	})
	if err != nil {
		return nil, err
	}
	if u.Path != "" {
		return nil, fmt.Errorf("URL must have an empty path (after trimming trailing slashes) but got %#v", u.Path)
	}
	return u, nil
}
