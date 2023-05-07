package fs

import (
	"errors"
	"fmt"
	"strings"
)

// validateName returns an error if the specified object name is incompatible with *FSStorage.
func validateName(name string) error {
	for _, part := range strings.Split(name, "/") {
		if err := validateNamePart(part); err != nil {
			return fmt.Errorf("name has invalid slash-delimited part: %w", err)
		}
	}
	return nil
}

func validateNamePart(part string) error {
	switch part {
	case "":
		// This makes object names invalid that:
		// - Contain consecutive slashes.
		// - Have a leading slash.
		// - Have a trailing slash.
		// - Are empty.
		return errors.New(`part is empty`)
	case ".", "..":
		return fmt.Errorf(`part is ".." or "."`)
	}
	if strings.HasSuffix(part, linkSuffix) {
		return fmt.Errorf(`part has suffix %#v`, linkSuffix)
	}
	if strings.Contains(part, "\\") {
		return fmt.Errorf(`part contains backslash`)
	}
	return nil
}
