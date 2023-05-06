package github

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v52/github"
	log "github.com/sirupsen/logrus"

	"github.com/go-mod-proxy/go-mod-proxy/internal/config"
)

type GitHubClientManagerOptions struct {
	Instances []*config.GitHubInstance
	Transport http.RoundTripper
}

type GitHubClientManager struct {
	gitHubAppInstallationsCacheMaxAge time.Duration
	instancesByHost                   map[string]*gitHubInstance
}

func NewGitHubClientManager(opts GitHubClientManagerOptions) (*GitHubClientManager, error) {
	if opts.Transport == nil {
		return nil, fmt.Errorf("opts.Transport must not be nil")
	}
	g := &GitHubClientManager{
		gitHubAppInstallationsCacheMaxAge: time.Minute * 5,
		instancesByHost:                   map[string]*gitHubInstance{},
	}
	for i, configInstance := range opts.Instances {
		if configInstance == nil {
			return nil, fmt.Errorf("opts.Instances[%d] must not be nil", i)
		}
		if configInstance.Host == "" {
			return nil, fmt.Errorf("opts.Instances[%d].Host must not be empty", i)
		}
		if _, ok := g.instancesByHost[configInstance.Host]; ok {
			return nil, fmt.Errorf("opts.Instances is invalid: no two elements can have the same .Host but two elements have .Host %#v",
				configInstance.Host)
		}
		instance := &gitHubInstance{
			gitHubApps: map[int64]*gitHubApp{},
			host:       configInstance.Host,
		}
		g.instancesByHost[instance.host] = instance
		for j, configGitHubApp := range configInstance.GitHubApps {
			if configGitHubApp == nil {
				return nil, fmt.Errorf("opts.Instances[%d].GitHubApps[%d] must not be nil", i, j)
			}
			if configGitHubApp.PrivateKeyParsed == nil {
				return nil, fmt.Errorf("opts.Instances[%d].GitHubApps[%d].PrivateKeyParsed must not be nil", i, j)
			}
			if _, ok := instance.gitHubApps[configGitHubApp.ID]; ok {
				return nil, fmt.Errorf("opts.Instances[%d].GitHubApps is invalid: no two elements can have the same .ID but two elements have .ID %d",
					i, configGitHubApp.ID)
			}
			gitHubApp := &gitHubApp{
				id: configGitHubApp.ID,
			}
			instance.gitHubApps[gitHubApp.id] = gitHubApp
			gitHubApp.transport = ghinstallation.NewAppsTransportFromPrivateKey(opts.Transport, gitHubApp.id, configGitHubApp.PrivateKeyParsed)
			gitHubApp.client = github.NewClient(&http.Client{
				Transport: gitHubApp.transport,
			})
		}
	}
	return g, nil
}

func (g *GitHubClientManager) ensureGitHubAppInstallations(ctx context.Context, gitHubApp *gitHubApp) error {
	now := time.Now()
	if now.Sub(gitHubApp.installationsUpdateTime) <= g.gitHubAppInstallationsCacheMaxAge {
		return nil
	}
	newInstallations := map[string]*gitHubAppInstallation{}
	listOpts := &github.ListOptions{
		Page: 1,
		// https://docs.github.com/en/rest/reference/apps#list-installations-for-the-authenticated-app
		// As of 6 September 2020 100 is the maximum page size.
		PerPage: 100,
	}
	for {
		installations, resp, err := gitHubApp.client.Apps.ListInstallations(ctx, listOpts)
		if err != nil {
			return err
		}
		for _, installation := range installations {
			id := *installation.ID
			repoOwner := *installation.Account.Login
			oldInstallation := gitHubApp.installations[repoOwner]
			var newInstallation *gitHubAppInstallation
			if oldInstallation != nil && oldInstallation.id == id {
				newInstallation = oldInstallation
			} else {
				newInstallation = &gitHubAppInstallation{
					id: id,
				}
				newInstallation.transport = ghinstallation.NewFromAppsTransport(gitHubApp.transport, id)
				newInstallation.client = github.NewClient(&http.Client{
					Transport: newInstallation.transport,
				})
			}
			log.Debugf("got installation %d of repoOwner %#v", id, repoOwner)
			newInstallations[repoOwner] = newInstallation
		}
		logger := log.StandardLogger()
		if logLevel := log.TraceLevel; logger.IsLevelEnabled(logLevel) {
			nextPageStr := "nil"
			if resp.NextPage != 0 {
				nextPageStr = strconv.FormatInt(int64(resp.NextPage), 10)
			}
			lastPageStr := "nil"
			if resp.LastPage != 0 {
				lastPageStr = strconv.FormatInt(int64(resp.LastPage), 10)
			}
			log.NewEntry(logger).Logf(logLevel, "request page = %d, response nextPage = %s, response lastPage = %s", listOpts.Page, nextPageStr, lastPageStr)
			log.NewEntry(logger).Logf(logLevel, "response header Link: %s", strings.Join(resp.Response.Header["Link"], ", "))
		}
		if resp.NextPage == 0 {
			break
		}
		listOpts.Page++
	}
	gitHubApp.installations = newInstallations
	gitHubApp.installationsUpdateTime = now
	return nil
}

func (g *GitHubClientManager) getGitHubAppInstallation(ctx context.Context, gitHubApp *gitHubApp,
	repoOwner string) (*gitHubAppInstallation, error) {
	gitHubApp.mu.Lock()
	defer gitHubApp.mu.Unlock()
	err := g.ensureGitHubAppInstallations(ctx, gitHubApp)
	if err != nil {
		return nil, err
	}
	i := gitHubApp.installations[repoOwner]
	if i == nil {
		now := time.Now()
		t := g.gitHubAppInstallationsCacheMaxAge - now.Sub(gitHubApp.installationsUpdateTime)
		return nil, notDefinedErrorf("GitHub App with id %d is not installed on repoOwner %#v (next installation cache refresh in %.2f seconds)",
			gitHubApp.id, repoOwner, t.Seconds())
	}
	return i, nil
}

func (g *GitHubClientManager) getInstance(host string) (*gitHubInstance, error) {
	instance := g.instancesByHost[host]
	if instance != nil {
		return instance, nil
	}
	return nil, notDefinedErrorf("no GitHub instance with host %#v is known", host)
}

func (g *GitHubClientManager) getInstanceGitHubApp(instance *gitHubInstance, appID int64) (*gitHubApp, error) {
	gitHubApp := instance.gitHubApps[appID]
	if gitHubApp == nil {
		return nil, notDefinedErrorf("no GitHub App with id %d is known (for the GitHub instance with host %#v)", appID, instance.host)
	}
	return gitHubApp, nil
}

// GetGitHubAppClient returns a NotDefinedError if:
//  1. no GitHub instance with host host is defined
//  2. no GitHub App with id appID is defined in GitHub instance with host host
//  3. the GitHub App with id appID in GitHub instance with host host is not installed in account repoOwner (and no error occurred
//     attempting to query GitHub App installations)
func (g *GitHubClientManager) GetGitHubAppClient(ctx context.Context, host string, appID int64, repoOwner string) (*github.Client,
	TokenGetter, error) {
	instance, err := g.getInstance(host)
	if err != nil {
		return nil, nil, err
	}
	gitHubApp, err := g.getInstanceGitHubApp(instance, appID)
	if err != nil {
		return nil, nil, err
	}
	gitHubAppInstallation, err := g.getGitHubAppInstallation(ctx, gitHubApp, repoOwner)
	if err != nil {
		return nil, nil, err
	}
	return gitHubAppInstallation.client, gitHubAppInstallation.transport.Token, nil
}

type gitHubInstance struct {
	gitHubApps map[int64]*gitHubApp
	host       string
}

type gitHubApp struct {
	client    *github.Client
	id        int64
	transport *ghinstallation.AppsTransport

	installations           map[string]*gitHubAppInstallation
	installationsUpdateTime time.Time
	// mu is a mutex for reading/updating/initializing installations
	mu sync.Mutex
}

type gitHubAppInstallation struct {
	client    *github.Client
	id        int64
	transport *ghinstallation.Transport
}

type TokenGetter = func(ctx context.Context) (string, error)

type NotDefinedError struct {
	s string
}

func notDefinedErrorf(format string, args ...interface{}) *NotDefinedError {
	return &NotDefinedError{
		s: fmt.Sprintf(format, args...),
	}
}

func (n *NotDefinedError) Error() string {
	return n.s
}
