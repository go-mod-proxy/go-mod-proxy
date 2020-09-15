package credentialhelper

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-mod-proxy/go/internal/pkg/git"
	credentialhelpergit "github.com/go-mod-proxy/go/internal/pkg/server/credentialhelper/git"
	"github.com/go-mod-proxy/go/internal/pkg/util"
)

func runGit(ctx context.Context, goModulePath string, port int, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("unexpectedly got %d positional arguments when 1 is expected", len(args))
	}
	if args[0] != "get" {
		return fmt.Errorf(`%s positional argument must be equal to "get" but got %#v`, util.FormatIth(1), args[0])
	}
	c, err := git.ParseCredentialHelperStdin(os.Stdin)
	if err != nil {
		return err
	}
	_ = os.Stdin.Close()
	if !util.PathIsLexicalDescendant(goModulePath, c.Host+"/"+c.Path) {
		return fmt.Errorf("stdin (hosst = %#v, path = %#v) is unexpectedly inconsistent with Go module path flag %#v", c.Host,
			c.Path, goModulePath)
	}
	respBody := &credentialhelpergit.UserPassword{}
	err = doRequest(ctx, port, goModulePath, respBody)
	if err != nil {
		return err
	}
	// Sanity checks to get good error messages instead of mangling stdout encoding
	if respBody.User == "" {
		panic(fmt.Errorf("user is unexpectedly empty"))
	}
	if strings.ContainsAny(respBody.User, "\n") {
		panic(fmt.Errorf("user unexpectedly contains a line feed character"))
	}
	if respBody.Password == "" {
		panic(fmt.Errorf("password is unexpectedly empty"))
	}
	if strings.ContainsAny(respBody.Password, "\n") {
		panic(fmt.Errorf("password unexpectedly contains a line feed character"))
	}
	stdout := fmt.Sprintf("username=%s\npassword=%s\n", respBody.User, respBody.Password)
	_, err = os.Stdout.Write([]byte(stdout))
	return err
}
