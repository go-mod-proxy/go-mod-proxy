package gocmd

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/alessio/shellescape"
)

func formatArgs(args []string) string {
	var sb strings.Builder
	sb.WriteString(shellescape.Quote(args[0]))
	for i := 1; i < len(args); i++ {
		arg := args[i]
		sb.WriteByte(' ')
		sb.WriteString(shellescape.Quote(arg))
	}
	return sb.String()
}

func fdSeekToStart(fd *os.File) error {
	offset, err := fd.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	if offset != 0 {
		return fmt.Errorf("(*os.File).Seek(0, io.SeekStart) returned unexpected non-zero offset %d", offset)
	}
	return nil
}

// looksLikeGoogleVirtualPrivateCloudError tests if errorString ends with something like:
// "reading https://proxy.golang.org/google.golang.org/api/@v/v0.8.0.zip: 403 Forbidden"
func looksLikeGoogleVirtualPrivateCloudError(errorString string) bool {
	rest := errorString
	i := strings.LastIndexByte(rest, ':')
	if i < 0 {
		return false
	}
	part := rest[i+1:]
	part = strings.Trim(part, " ")
	rest = rest[:i]
	if !strings.EqualFold(part, "403 Forbidden") {
		return false
	}
	i = strings.LastIndexByte(rest, ':')
	if i < 0 {
		return false
	}
	j := strings.LastIndexByte(rest[:i], ':')
	if j < 0 {
		part = rest
	} else {
		part = rest[j+1:]
	}
	part = strings.Trim(part, " ")
	i = strings.IndexByte(part, ' ')
	if i < 0 {
		return false
	}
	partPrefix := part[:i]
	if !strings.EqualFold(partPrefix, "reading") {
		return false
	}
	partSuffix := strings.TrimLeft(part[i+1:], " ")
	if partSuffixURL, err := url.Parse(partSuffix); err != nil {
		return false
	} else {
		partSuffixURL.User = nil
		if strings.HasPrefix(partSuffixURL.String(), "https://proxy.golang.org/google.golang.org/") {
			return true
		}
	}
	return false
}
