package credentialhelper

import (
	"context"
	"fmt"
)

// CLI is a type reflected by "github.com/alecthomas/kong" that configures the CLI command for the client forward proxy.
//
//nolint:govet // linter does not like the syntax required by the kong package
type CLI struct {
	GoModulePath string   `required help:"Go module path"`
	Port         int      `required help:"Port on 127.0.0.1 that the server is listening on"`
	Type         string   `required help:"Type of credential helper. Must be case-sensitive equal to git"`
	Args         []string `arg`
}

func Run(ctx context.Context, opts *CLI) error {
	if ctx == nil {
		return fmt.Errorf("ctx must not be nil")
	}
	if opts.Port <= 0 {
		return fmt.Errorf("value of port flag must be positive")
	}
	switch opts.Type {
	case "git":
		return runGit(ctx, opts.GoModulePath, opts.Port, opts.Args)
	default:
		return fmt.Errorf(`value of type flag must be case-sensitive equal to "git" but got %#v`, opts.Type)
	}
}
