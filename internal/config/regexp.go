package config

import (
	"regexp"
)

// Regexp is a wrapper for a *regexp.Regexp that implements the Unmarshaler interface.
type Regexp struct {
	Value *regexp.Regexp
}

// UnmarshalYAML implements the Unmarshaler interface.
func (c *Regexp) UnmarshalYAML(unmarshal func(any) error) error {
	var expr string
	if err := unmarshal(&expr); err != nil {
		return err
	}
	var err error
	c.Value, err = regexp.Compile(expr)
	return err
}
