package main

import (
	"context"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	log "github.com/sirupsen/logrus"

	"github.com/go-mod-proxy/go/cmd/clientforwardproxy"
	"github.com/go-mod-proxy/go/cmd/credentialhelper"
	"github.com/go-mod-proxy/go/cmd/server"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)
	log.SetFormatter(&log.TextFormatter{
		ForceColors: true,
	})
	if err := mainCore(); err != nil {
		log.Fatal(err)
	}
}

// CLI is a definition for kong command line parser
var CLI struct {
	LogLevel string `help:"Log level. Must be case-insensitive equal to one of trace, debug, info, warning, error, panic and fatal"`

	ClientForwardProxy clientforwardproxy.CLI `cmd`
	CredentialHelper   credentialhelper.CLI   `cmd help:"Credential helper utility used by server"`
	Server             server.CLI             `cmd`
}

func mainCore() error {
	ctx := context.Background()
	// TODO cancel ctx on signals such as SIGINT
	kongCtx := kong.Parse(&CLI)
	if CLI.LogLevel != "" {
		if logLevel, err := log.ParseLevel(CLI.LogLevel); err != nil {
			return fmt.Errorf(`value of log level flag must be case-insensitive equal to one of trace, debug, info, warning, error, panic `+
				`and fatal but got %#v`, CLI.LogLevel)
		} else {
			log.SetLevel(logLevel)
		}
	}
	switch kongCtx.Command() {
	case "client-forward-proxy":
		return clientforwardproxy.Run(ctx, &CLI.ClientForwardProxy)
	case "credential-helper <args>":
		log.SetOutput(os.Stderr)
		return credentialhelper.Run(ctx, &CLI.CredentialHelper)
	case "server":
		return server.Run(ctx, &CLI.Server)
	default:
		panic(kongCtx.Command())
	}
	return nil
}
