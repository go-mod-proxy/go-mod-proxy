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

	"github.com/jbrekelmans/go-module-proxy/internal/pkg/config"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/github"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/server"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/server/credentialhelper"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/service/auth"
	serviceauthaccesstoken "github.com/jbrekelmans/go-module-proxy/internal/pkg/service/auth/accesstoken"
	serviceauthgce "github.com/jbrekelmans/go-module-proxy/internal/pkg/service/auth/gce"
	servicegomodulegocmd "github.com/jbrekelmans/go-module-proxy/internal/pkg/service/gomodule/gocmd"
	servicestorage "github.com/jbrekelmans/go-module-proxy/internal/pkg/service/storage"
	servicestoragegcs "github.com/jbrekelmans/go-module-proxy/internal/pkg/service/storage/gcs"
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
	httpTransport := cleanhttp.DefaultTransport()
	httpProxyInfo, err := config.GetHTTPProxyInfoAndUnsetEnviron(cfg)
	if err != nil {
		return err
	}
	httpTransport.Proxy = func(req *http.Request) (*url.URL, error) {
		return httpProxyInfo.ProxyFunc(req.URL)
	}
	httpClient := &http.Client{
		Transport: httpTransport,
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
	if enableGCEAuth || cfg.Storage.GCS != nil {
		googleCredentials, err = google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return err
		}
		googleHTTPClient = oauth2.NewClient(nil, googleCredentials.TokenSource)
		// A slightly hacky way to reuse httpClient's Transport instead of having another one.
		googleHTTPClient.Transport.(*oauth2.Transport).Base = httpClient.Transport
	}
	var storage servicestorage.Storage
	if cfg.Storage.GCS != nil {
		gcsClient, err := gcs.NewClient(ctx, option.WithHTTPClient(googleHTTPClient))
		if err != nil {
			return err
		}
		storage, err = servicestoragegcs.NewStorage(servicestoragegcs.StorageOptions{
			Bucket:     cfg.Storage.GCS.Bucket,
			GCSClient:  gcsClient,
			HTTPClient: googleHTTPClient,
		})
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("non-GCS storage is not implemented")
	}
	var instanceIdentityVerifier *jaspercompute.InstanceIdentityVerifier
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
		instanceIdentityVerifier, err = jaspercompute.NewInstanceIdentityVerifier(
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
	}
	identityStore, err := auth.NewInMemoryIdentityStore()
	if err != nil {
		return err
	}
	for _, identity := range cfg.ClientAuth.Identities {
		err := identityStore.Add(identity)
		if err != nil {
			return err
		}
	}
	accessTokenAuth, err := serviceauthaccesstoken.NewAuthenticator(
		cfg.ClientAuth.Authenticators.AccessToken.Audience,
		[]byte(cfg.ClientAuth.Authenticators.AccessToken.Secret),
		cfg.ClientAuth.Authenticators.AccessToken.TimeToLive,
		identityStore,
	)
	realm := ""
	var gceAuth *serviceauthgce.Authenticator
	if instanceIdentityVerifier != nil {
		var err error
		gceAuth, err = serviceauthgce.NewAuthenticator(instanceIdentityVerifier, identityStore)
		if err != nil {
			return err
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
	var parentProxy *url.URL
	if cfg.ParentProxy != nil {
		parentProxy = cfg.ParentProxy.URLParsed
	}
	goModuleService, err := servicegomodulegocmd.NewService(servicegomodulegocmd.ServiceOptions{
		GitCredentialHelperShell: fmt.Sprintf("exec %s --log-level=%s credential-helper --type=git --port=%d",
			shellescape.Quote(executable2),
			log.GetLevel().String(),
			opts.CredentialHelperPort),
		HTTPProxyInfo:       httpProxyInfo,
		MaxParallelCommands: cfg.MaxChildProcesses,
		ParentProxy:         parentProxy,
		PrivateModules:      cfg.PrivateModules,
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
