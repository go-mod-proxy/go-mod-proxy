package validation

import (
	"fmt"
	"net"
)

// ValidateHost validates and canonicalizes a host.
// TODO finish this
// Talk about canonicalization of cache keys.
// Is this required at all for git credential helpers? It seems safe to run "net/idna".ToASCII first.
//
//	Figure out if "net/http"'s conditional ToASCII call (see idnaASCII in https://golang.org/src/net/http/request.go?s=23078:23124)
//	is needed by us too.
//
// 1. Ports complicate canonicalization logic because in general we don't want to know about protocols used.
// 2. Go requires a dot in the host.
// It is stricter than what is accepted by net.Dial* functions (and (*net.Dialer).Dial* functions).
func ValidateHost(host string) (string, error) {
	hostname, port, err := net.SplitHostPort(host)
	if err == nil {
		return "", fmt.Errorf("host is not allowed to contain ports")
	}
	_ = hostname
	_ = port
	return hostname, nil
}
