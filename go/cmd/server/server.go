package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	gcs "cloud.google.com/go/storage"
	"github.com/alessio/shellescape"
	"github.com/hashicorp/go-cleanhttp"
	jaspergoogle "github.com/jbrekelmans/go-lib/auth/google"
	jaspercompute "github.com/jbrekelmans/go-lib/auth/google/compute"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/iam/v1"
	"google.golang.org/api/option"

	"github.com/go-mod-proxy/go/internal/pkg/config"
	"github.com/go-mod-proxy/go/internal/pkg/github"
	"github.com/go-mod-proxy/go/internal/pkg/server"
	"github.com/go-mod-proxy/go/internal/pkg/server/credentialhelper"
	"github.com/go-mod-proxy/go/internal/pkg/service/auth"
	serviceauthaccesstoken "github.com/go-mod-proxy/go/internal/pkg/service/auth/accesstoken"
	serviceauthgce "github.com/go-mod-proxy/go/internal/pkg/service/auth/gce"
	servicegomodulegocmd "github.com/go-mod-proxy/go/internal/pkg/service/gomodule/gocmd"
	servicestorage "github.com/go-mod-proxy/go/internal/pkg/service/storage"
	servicestoragegcs "github.com/go-mod-proxy/go/internal/pkg/service/storage/gcs"
)

// CLI is a type reflected by "github.com/alecthomas/kong" that configures the CLI command for the client forward proxy.
type CLI struct {
	ConfigFile           string `required type:"existingfile" help:"Name of the YAML config file"`
	Port                 int    `required help:"Port to listen on"`
	CredentialHelperPort int    `required help:"Credential helper port to listen on"`
	ScratchDir           string `help:"Directory in which to create temporary scratch files"`
}

func Run(ctx context.Context, opts *CLI) error {
	if opts.Port <= 0 {
		return fmt.Errorf("value of port flag musts be positive")
	}
	if opts.CredentialHelperPort <= 0 {
		return fmt.Errorf("value of credential helper port flag must be positive")
	}
	if opts.Port == opts.CredentialHelperPort {
		return fmt.Errorf("value of port flag and credential helper port flag must be different")
	}
	executable1, err := os.Executable()
	if err != nil {
		return err
	}
	executable2, err := filepath.EvalSymlinks(executable1)
	if err != nil {
		return fmt.Errorf("error doing eval symlinks on %#v: %w", executable1, err)
	}
	if !filepath.IsAbs(executable2) {
		panic(fmt.Errorf("file name of executable is unexpectedly not absolute"))
	}
	cfg, err := config.LoadFromYAMLFile(opts.ConfigFile)
	if err != nil {
		return err
	}
	if cfg.ClientAuth.Enabled && (cfg.ClientAuth.Authenticators == nil || cfg.ClientAuth.Authenticators.AccessToken == nil) {
		return fmt.Errorf("invalid configuration: if .clientAuth.enabled is true then .clientAuth.authenticators.accessToken must not be nil")
	}
	httpTransport := cleanhttp.DefaultPooledTransport()
	httpProxyInfo, err := config.GetHTTPProxyInfoAndUnsetEnviron(cfg)
	if err != nil {
		return err
	}
	if httpProxyInfo.ProxyFunc != nil {
		httpTransport.Proxy = func(req *http.Request) (*url.URL, error) {
			return httpProxyInfo.ProxyFunc(req.URL)
		}
	} else {
		httpTransport.Proxy = nil
	}
	httpClient := &http.Client{
		Transport: httpTransport,
	}
	httpTransportBypassProxy := cleanhttp.DefaultPooledTransport()
	httpTransportBypassProxy.Proxy = nil
	httpClientBypassProxy := &http.Client{
		Transport: httpTransportBypassProxy,
	}
	var enableGCEAuth bool
	if cfg.ClientAuth.Enabled {
		for _, identity := range cfg.ClientAuth.Identities {
			if identity.GCEInstanceIdentityBinding != nil {
				enableGCEAuth = true
				break
			}
		}
	}
	var googleCredentials *google.Credentials
	var googleHTTPClient *http.Client
	var googleHTTPClientBypassProxy *http.Client
	if enableGCEAuth || cfg.Storage.GCS != nil {
		googleCredentials, err = google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return err
		}
		googleHTTPClient = oauth2.NewClient(nil, googleCredentials.TokenSource)
		// A slightly hacky way to reuse httpClient's Transport instead of having another one.
		googleHTTPClient.Transport.(*oauth2.Transport).Base = httpClient.Transport
		googleHTTPClientBypassProxy = oauth2.NewClient(nil, googleCredentials.TokenSource)
		// A slightly hacky way to reuse httpClient's Transport instead of having another one.
		googleHTTPClientBypassProxy.Transport.(*oauth2.Transport).Base = httpClientBypassProxy.Transport
	}
	var storage servicestorage.Storage
	if cfg.Storage.GCS != nil {
		storageHTTPClient := googleHTTPClient
		if cfg.Storage.GCS.BypassHTTPProxy {
			storageHTTPClient = googleHTTPClientBypassProxy
		}
		gcsClient, err := gcs.NewClient(ctx, option.WithHTTPClient(storageHTTPClient))
		if err != nil {
			return err
		}
		storage, err = servicestoragegcs.NewStorage(servicestoragegcs.StorageOptions{
			Bucket:     cfg.Storage.GCS.Bucket,
			GCSClient:  gcsClient,
			HTTPClient: storageHTTPClient,
		})
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("non-GCS storage is not implemented")
	}
	var accessTokenAuth *serviceauthaccesstoken.Authenticator
	var gceAuth *serviceauthgce.Authenticator
	var identityStore auth.IdentityStore
	realm := ""
	if cfg.ClientAuth.Enabled {
		var err error
		identityStore, err = auth.NewInMemoryIdentityStore()
		if err != nil {
			return err
		}
		for _, identity := range cfg.ClientAuth.Identities {
			err := identityStore.Add(identity)
			if err != nil {
				return err
			}
		}
		accessTokenAuth, err = serviceauthaccesstoken.NewAuthenticator(
			cfg.ClientAuth.Authenticators.AccessToken.Audience,
			cfg.ClientAuth.Authenticators.AccessToken.Secret.Plaintext,
			cfg.ClientAuth.Authenticators.AccessToken.TimeToLive,
			identityStore,
		)
		if !enableGCEAuth {
			log.Infof("not enabling GCE authentication because .clientAuth.enabled is not true or no element of .clientAuth.identities in %#v has .gceInstanceIdentityBinding != null",
				opts.ConfigFile)
		} else {
			log.Infof("enabling GCE authentication because an element of .clientAuth.identities in %#v has .gceInstanceIdentityBinding != null",
				opts.ConfigFile)
			iamService, err := iam.NewService(ctx, option.WithHTTPClient(googleHTTPClient))
			if err != nil {
				return err
			}
			validatorUsingGoogle, err := config.NewValidatorUsingGoogle(ctx, cfg, iamService)
			if err != nil {
				return err
			}
			if err := validatorUsingGoogle.Run(); err != nil {
				return fmt.Errorf("error validating file %#v: %w", opts.ConfigFile, err)
			}
			computeService, err := compute.NewService(ctx, option.WithHTTPClient(googleHTTPClient))
			if err != nil {
				return err
			}
			keySetProvider := jaspergoogle.CachingKeySetProvider(
				jaspergoogle.DefaultCachingKeySetProviderTimeToLive,
				jaspergoogle.HTTPSKeySetProvider(httpClient),
			)
			instanceIdentityVerifier, err := jaspercompute.NewInstanceIdentityVerifier(
				cfg.ClientAuth.Authenticators.GCEInstanceIdentity.Audience,
				jaspercompute.WithInstanceGetter(func(ctx context.Context, projectID, zone, instanceName string) (*compute.Instance, error) {
					return computeService.Instances.Get(projectID, zone, instanceName).Context(ctx).Do()
				}),
				jaspercompute.WithServiceAccountGetter(func(ctx context.Context, name string) (*iam.ServiceAccount, error) {
					return iamService.Projects.ServiceAccounts.Get(name).Context(ctx).Do()
				}),
				jaspercompute.WithKeySetProvider(keySetProvider),
			)
			if err != nil {
				return err
			}
			gceAuth, err = serviceauthgce.NewAuthenticator(instanceIdentityVerifier, identityStore)
			if err != nil {
				return err
			}
		}
	}
	gitHubClientManager, err := github.NewGitHubClientManager(github.GitHubClientManagerOptions{
		Instances: cfg.GitHub,
		Transport: httpClient.Transport,
	})
	if err != nil {
		return err
	}
	credentialHelperServer, err := credentialhelper.NewServer(credentialhelper.ServerOptions{
		GitHubClientManager: gitHubClientManager,
		PrivateModules:      cfg.PrivateModules,
	})
	if err != nil {
		return err
	}
	if err := credentialHelperServer.Start(ctx, opts.CredentialHelperPort); err != nil {
		return err
	}
	defer credentialHelperServer.Stop()
	scratchDir := opts.ScratchDir
	if scratchDir == "" {
		scratchDir = os.TempDir()
	}
	goModuleService, err := servicegomodulegocmd.NewService(servicegomodulegocmd.ServiceOptions{
		GitCredentialHelperShell: fmt.Sprintf("exec %s --log-level=%s credential-helper --type=git --port=%d",
			shellescape.Quote(executable2),
			log.GetLevel().String(),
			opts.CredentialHelperPort),
		HTTPProxyInfo:       httpProxyInfo,
		HTTPTransport:       httpTransport,
		MaxParallelCommands: cfg.MaxChildProcesses,
		ParentProxy:         cfg.ParentProxy.URLParsed,
		PrivateModules:      cfg.PrivateModules,
		PublicModules:       &cfg.PublicModules,
		ScratchDir:          scratchDir,
		Storage:             storage,
	})
	if err != nil {
		return err
	}
	server, err := server.NewServer(server.ServerOptions{
		AccessControlList:        cfg.ClientAuth.AccessControlList,
		AccessTokenAuthenticator: accessTokenAuth,
		ClientAuthEnabled:        cfg.ClientAuth.Enabled,
		GCEAuthenticator:         gceAuth,
		GoModuleService:          goModuleService,
		IdentityStore:            identityStore,
		Realm:                    realm,
		SumDatabaseProxy:         cfg.SumDatabaseProxy,
		Transport:                httpTransport,
	})
	if err != nil {
		return err
	}
	err = http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", opts.Port), server)
	if err != http.ErrServerClosed {
		return err
	}
	return nil
}
