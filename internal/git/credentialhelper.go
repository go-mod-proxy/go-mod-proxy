package git

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	log "github.com/sirupsen/logrus"
)

const protocolHTTPS = "https"

type CredentialHelperParams struct {
	Host string `json:"host"`
	Path string `json:"path"`
}

func ParseCredentialHelperStdin(stdin io.Reader) (*CredentialHelperParams, error) {
	// TODO encode other fields and turn assumptions that protocol is "https" and username is illegal into a check outside of this
	// package.
	c := &CredentialHelperParams{}
	keys := map[string]struct{}{}
	stdinScanner := bufio.NewScanner(stdin)
	for stdinScanner.Scan() {
		line := stdinScanner.Text()
		log.Tracef("got stdin line: %s", line)
		i := strings.IndexByte(line, '=')
		if i < 0 {
			return nil, fmt.Errorf(`stdin has a line (%#v) that unexpectedly does not contain a "=" character`, line)
		}
		key := line[:i]
		if _, ok := keys[key]; ok {
			return nil, fmt.Errorf(`stdin unexpectedly has multiple lines starting with %#v`, key+"=")
		}
		keys[key] = struct{}{}
		switch key {
		case "protocol":
			protocol := line[i+1:]
			if protocol != protocolHTTPS {
				return nil, fmt.Errorf(`stdin has a line (%#v) starting with %#v but the protocol is invalid or unsupported (only %#v is supported)`,
					line, key+"=", protocolHTTPS)
			}
		case "host":
			c.Host = line[i+1:]
		case "path":
			c.Path = line[i+1:]
		case "username":
			return nil, fmt.Errorf(`stdin has a line (%#v) starting with %#v but a username is not expected`, line, key+"=")
		default:
			return nil, fmt.Errorf(`stdin has a line (%#v) that unexpectedly does not start with one of "protocol=", "host=", "path=" and "username="`,
				line)
		}
	}
	if err := stdinScanner.Err(); err != nil {
		return nil, err
	}
	for _, key := range []string{"protocol", "host", "path"} {
		if _, ok := keys[key]; !ok {
			return nil, fmt.Errorf(`stdin is unexpectedly missing a line starting with %#v`, key+"=")
		}
	}
	return c, nil
}
