package httpproxy

import (
	"golang.org/x/net/idna"
)

// idnaASCII normalizes a hostname with an algorithm equivalent to Go's "http" and
// "http/httpproxy" packages.
func idnaASCII(hostname string) (string, error) {
	// TODO: Consider removing this check after verifying performance is okay.
	// Right now punycode verification, length checks, context checks, and the
	// permissible character tests are all omitted. It also prevents the ToASCII
	// call from salvaging an invalid IDN, when possible. As a result it may be
	// possible to have two IDNs that appear identical to the user where the
	// ASCII-only version causes an error downstream whereas the non-ASCII
	// version does not.
	// Note that for correct ASCII IDNs ToASCII will only do considerably more
	// work, but it will not cause an allocation.
	if isASCII(hostname) {
		return hostname, nil
	}
	return idna.Lookup.ToASCII(hostname)
}
