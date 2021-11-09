package gocmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	log "github.com/sirupsen/logrus"
	module "golang.org/x/mod/module"

	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/config"
	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/git"
	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/modproxyclient"
	gomoduleservice "github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/service/gomodule"
	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/service/storage"
	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/util"
)

const (
	storageConcatObjNamePrefix           = "concat/"
	storageGoModObjNamePrefix            = "gomod/"
	storageZipObjNamePrefix              = "zip/"
	storageGoModObjCommitTimeMetadataKey = "gomod-commit-time"
)

type goModuleInfo struct {
	Path  string
	Error *struct {
		Err string
	} // error loading module
	Version  string
	Versions []string
	// Info is the path to a file containing JSON marshalling of a "github.com/go-mod-proxy/go-mod-proxy/go/internal/app/service/gomodule".Info value.
	Info  string
	GoMod string
	// Zip is the path to a file containing Zip file
	Zip       string
	Dir       string
	Sum       string
	Time      time.Time
	GoModSum  string
	GoVersion string
}

type ServiceOptions struct {
	GitCredentialHelperShell          string
	HTTPProxyInfo                     *config.HTTPProxyInfo
	HTTPTransport                     http.RoundTripper
	MaxParallelCommands               int
	ParentProxy                       *url.URL
	PrivateModules                    []*config.PrivateModulesElement
	PublicModules                     *config.PublicModules
	ReadAfterListIsStronglyConsistent bool
	ScratchDir                        string
	Storage                           storage.Storage
}

type Service struct {
	envGoProxy                        string
	gitCredentialHelperShell          string
	goBinFile                         string
	httpClient                        *http.Client
	httpProxyInfo                     *config.HTTPProxyInfo
	parentProxyURL                    string
	privateModules                    []*config.PrivateModulesElement
	publicModulesGoSumDBEnvVar        string
	readAfterListIsStronglyConsistent bool
	runCmdResourcePool                *maxParallelismResourcePool
	scratchDir                        string
	storage                           storage.Storage
	tempGoEnvBaseEnviron              *util.Environ
}

var _ gomoduleservice.Service = (*Service)(nil)

// NewService creates a new Go module service.
func NewService(opts ServiceOptions) (s *Service, err error) {
	if opts.GitCredentialHelperShell == "" {
		return nil, fmt.Errorf("opts.GitCredentialHelperShell must not be empty")
	} else if strings.IndexByte(opts.GitCredentialHelperShell, 0) >= 0 {
		return nil, fmt.Errorf("opts.GitCredentialHelperShell illegally contains zero byte")
	}
	if opts.HTTPProxyInfo == nil {
		return nil, fmt.Errorf("opts.HTTPProxyInfo must not be nil")
	}
	if opts.HTTPTransport == nil {
		return nil, fmt.Errorf("opts.HTTPTransport must not be nil")
	}
	if opts.ParentProxy == nil {
		return nil, fmt.Errorf("opts.ParentProxy must not be nil")
	}
	if opts.PublicModules == nil {
		return nil, fmt.Errorf("opts.PublicModules must not be nil")
	}
	if opts.Storage == nil {
		return nil, fmt.Errorf("opts.Storage must not be nil")
	}
	scratchDir1, err := filepath.Abs(opts.ScratchDir)
	if err != nil {
		return nil, fmt.Errorf("error making file name %#v absolute: %w", opts.ScratchDir, err)
	}
	scratchDir2, err := filepath.EvalSymlinks(scratchDir1)
	if err != nil {
		return nil, fmt.Errorf("error doing eval symlinks on %#v: %w", scratchDir1, err)
	}
	goBinFile1, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf(`could not find "go" binary on PATH: %w`, err)
	}
	goBinFile2, err := filepath.EvalSymlinks(goBinFile1)
	if err != nil {
		return nil, fmt.Errorf("error doing eval symlinks on %#v: %w", goBinFile1, err)
	}
	maxParallelCommands := opts.MaxParallelCommands
	if maxParallelCommands == 0 {
		maxParallelCommands = 20
	} else if maxParallelCommands < 0 {
		return nil, fmt.Errorf("opts.MaxParallelCommands must be non-negative")
	}
	runCmdResourcePool, err := newMaxParallelismResourcePool(maxParallelCommands)
	if err != nil {
		return nil, err
	}
	var publicModulesGoSumDBEnvVar string
	if opts.PublicModules.SumDatabase != nil {
		publicModulesGoSumDBEnvVar = opts.PublicModules.SumDatabase.FormatGoSumDBEnvVar()
	}
	ss := &Service{
		gitCredentialHelperShell: opts.GitCredentialHelperShell,
		goBinFile:                goBinFile2,
		httpClient: &http.Client{
			Transport: opts.HTTPTransport,
		},
		httpProxyInfo:                     opts.HTTPProxyInfo,
		privateModules:                    opts.PrivateModules,
		publicModulesGoSumDBEnvVar:        publicModulesGoSumDBEnvVar,
		scratchDir:                        scratchDir2,
		readAfterListIsStronglyConsistent: opts.ReadAfterListIsStronglyConsistent,
		runCmdResourcePool:                runCmdResourcePool,
		storage:                           opts.Storage,
		tempGoEnvBaseEnviron:              getTempGoEnvBaseEnviron(),
	}
	parentProxyStr := opts.ParentProxy.String()
	// , is valid in URLs, but illegal in GOPROXY environment variable
	if strings.ContainsAny(parentProxyStr, "|,") {
		return nil, fmt.Errorf(`opts.ParentProxy is invalid because opts.ParentProxy.String() contains illegal character "," or "|"`)
	}
	ss.envGoProxy = parentProxyStr + ",direct"
	ss.parentProxyURL = parentProxyStr + "/"
	// Sanity check to see if s.scratchDir is not within a Go module (otherwise this can interfere)
	args := []string{ss.goBinFile, "mod", "download"}
	t, err := ss.newTempGoEnv()
	if err != nil {
		return
	}
	defer func() {
		err2 := t.removeRef()
		if err2 != nil {
			if err == nil {
				err = err2
			} else {
				log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", t.TmpDir, err2)
			}
		}
	}()
	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second*20)
	defer cancelFunc()
	_, strLog, err := ss.runCmd(ctx, t, args)
	if err != nil {
		i := strings.Index(strLog, "stderr: go mod download: no modules specified")
		if i < 0 || (i > 0 && strLog[i-1] != '\n') {
			return
		}
		err = nil
		s = ss
	} else {
		err = fmt.Errorf("opts.ScratchDir appears invalid because the following command succeeded unexpectedly: %s", formatArgs(args))
	}
	return
}

func (s *Service) getGoModuleAndIndexIfNeeded(ctx context.Context, tempGoEnv *tempGoEnv,
	moduleVersion *module.Version, runCmdResource resource) (fSeeStorage bool, info *gomoduleservice.Info, downloadInfo *goModuleInfo, err error) {
	defer runCmdResource.release()
	err = s.initializeTempGoEnvForModule(ctx, tempGoEnv, moduleVersion.Path)
	if err != nil {
		return
	}
	args := []string{s.goBinFile, "mod", "download", "-json", moduleVersion.Path + "@" + moduleVersion.Version}
	stdout, strLog, err := s.runCmd(ctx, tempGoEnv, args)
	if err != nil {
		return
	}
	runCmdResource.release()
	downloadInfo = &goModuleInfo{}
	err = util.UnmarshalJSON(bytes.NewReader(stdout), downloadInfo, true)
	if err != nil {
		err = fmt.Errorf("command %s succeeded but got unexpected stderr/stdout:\n%s", formatArgs(args), strLog)
		return
	}
	if downloadInfo.Error != nil {
		err = fmt.Errorf("command %s succeeded but got unexpected error loading module:\n%s", formatArgs(args), strLog)
		return
	}
	infoJSONBytes, err := ioutil.ReadFile(downloadInfo.Info)
	if err != nil {
		err = fmt.Errorf(`unexpected error reading .info file created by %s command: %w`, formatArgs(args), err)
		return
	}
	info = &gomoduleservice.Info{}
	err = util.UnmarshalJSON(bytes.NewReader(infoJSONBytes), info, true)
	if err != nil {
		err = fmt.Errorf(`unexpected error JSON-unmarshalling contents of .info file created by %s command: %w`, formatArgs(args), err)
		return
	}

	if info.Time == (time.Time{}) {
		err = fmt.Errorf(".info file created by %s command contains JSON object that does not set .Time or sets .Time to an invalid value",
			formatArgs(args))
		return
	}

	// Do not index non-canonical versions. This should never happen but it's easier to reject, than to prove we never corrupted
	// our append-only storage.
	if info.Version == "" {
		err = fmt.Errorf(".info file created by %s command contains JSON object that does not set .Version (to a non-empty string)",
			formatArgs(args))
		return
	} else if infoVersionCanonical := module.CanonicalVersion(info.Version); infoVersionCanonical != info.Version {
		err = fmt.Errorf(".info file created by %s command contains JSON object with .Version = %#v that is not canonical (%#v)",
			formatArgs(args), info.Version, infoVersionCanonical)
		return
	}

	goModFD, err := newSharedFDOpen(downloadInfo.GoMod)
	if err != nil {
		err = fmt.Errorf(`unexpected error opening .mod file created by %s command: %w`, formatArgs(args), err)
		return
	}
	defer func() {
		err2 := goModFD.removeRef()
		if err2 != nil {
			if err == nil {
				err = err2
			} else {
				log.Errorf("*sharedFD(%p, %#v) error closing fd: %v", goModFD, goModFD.FD.Name(), err2)
			}
		}
	}()
	zipFD, err := newSharedFDOpen(downloadInfo.Zip)
	if err != nil {
		err = fmt.Errorf(`unexpected error opening .zip file created by %s command: %w`, formatArgs(args), err)
		return
	}
	defer func() {
		err2 := zipFD.removeRef()
		if err2 != nil {
			if err == nil {
				err = err2
			} else {
				log.Errorf("*sharedFD(%p, %#v) error closing fd: %v", zipFD, zipFD.FD.Name(), err2)
			}
		}
	}()
	readerForConcatObj, err := newReaderForCreateConcatObj(info.Time.UTC(), goModFD.FD, zipFD.FD)
	if err != nil {
		return
	}
	name := storageConcatObjNamePrefix + moduleVersion.Path + "@" + info.Version
	err = s.storage.CreateObjectExclusively(ctx, name, nil, readerForConcatObj)
	if err != nil {
		if !storage.ErrorIsCode(err, storage.PreconditionFailed) {
			err = mapStorageError(err)
			return
		}
		err = nil
		fSeeStorage = true
	} else {
		log.Infof("stored object %#v", name)
		goModFD.addRef()
		zipFD.addRef()
		tempGoEnv.addRef()
		go s.indexGoModule(tempGoEnv, info.Time, goModFD, zipFD, module.Version{
			Path:    moduleVersion.Path,
			Version: info.Version,
		})
	}
	return
}

func (s *Service) getPrivateModulesElement(modulePath string) *config.PrivateModulesElement {
	for _, privateModulesElement := range s.privateModules {
		if util.PathIsLexicalDescendant(modulePath, privateModulesElement.PathPrefix) {
			return privateModulesElement
		}
	}
	return nil
}

func (s *Service) GoMod(ctx context.Context, moduleVersion *module.Version) (data io.ReadCloser, err error) {
	err = s.notFoundOptimizations(moduleVersion.Path)
	if err != nil {
		return
	}
	fSeeStorage, err := s.versionPreamble(moduleVersion.Version, false)
	if err != nil {
		return
	}
	if fSeeStorage {
		data, err = s.storage.GetObject(ctx, storageGoModObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
		if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
			err = mapStorageError(err)
			return
		}
		data, err = s.goModFromConcatObj(ctx, moduleVersion)
		if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
			return
		}
	}
	tempGoEnv, err := s.newTempGoEnv()
	if err != nil {
		return
	}
	defer func() {
		err2 := tempGoEnv.removeRef()
		if err2 != nil {
			if err == nil {
				err = err2
			} else {
				log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", tempGoEnv.TmpDir, err2)
			}
		}
	}()
	runCmdResource, err := s.runCmdResourcePool.acquire(ctx)
	if err != nil {
		return
	}
	defer runCmdResource.release()
	fSeeStorage, info, downloadInfo, err := s.getGoModuleAndIndexIfNeeded(ctx, tempGoEnv, moduleVersion, runCmdResource)
	if err != nil {
		return
	}
	if !fSeeStorage {
		data, err = tempGoEnv.open(downloadInfo.GoMod)
		return
	}
	data, err = s.goModFromConcatObj(ctx, &module.Version{
		Path:    moduleVersion.Path,
		Version: info.Version,
	})
	if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
		return
	}
	data, err = s.storage.GetObject(ctx, storageGoModObjNamePrefix+moduleVersion.Path+"@"+info.Version)
	err = mapStorageError(err)
	return
}

func (s *Service) goModFromConcatObj(ctx context.Context, moduleVersion *module.Version) (d io.ReadCloser, err error) {
	data, err := s.storage.GetObject(ctx, storageConcatObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err != nil {
		err = mapStorageError(err)
		return
	}
	didPanic := true
	closeDataAlways := false
	defer func() {
		if didPanic || closeDataAlways || err != nil {
			err2 := data.Close()
			if err2 != nil {
				if err == nil {
					err = err2
				} else {
					log.Errorf("error closing %T: %v", data, err2)
				}
			}
		}
	}()
	_, goModPrefix, goModToRead, _, err := parseConcatObjCommon(data)
	if err != nil {
		err = fmt.Errorf("error parsing concat obj's data: %w", err)
		didPanic = false
		return
	}
	if goModToRead > 0 {
		goModSuffixReader := io.LimitReader(data, int64(goModToRead))
		d = util.NewConcatReader(goModPrefix, goModSuffixReader, nil, data.Close)
		didPanic = false
		return
	}
	closeDataAlways = true
	d = ioutil.NopCloser(bytes.NewReader(goModPrefix))
	didPanic = false
	return
}

func (s *Service) Info(ctx context.Context, moduleVersion *module.Version) (info *gomoduleservice.Info, err error) {
	err = s.notFoundOptimizations(moduleVersion.Path)
	if err != nil {
		return
	}
	fSeeStorage, err := s.versionPreamble(moduleVersion.Version, true)
	if err != nil {
		return
	}
	if fSeeStorage {
		info, err = s.infoFromGoModObj(ctx, moduleVersion)
		if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
			return
		}
		info, err = s.infoFromConcatObj(ctx, moduleVersion)
		if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
			return
		}
	}
	tempGoEnv, err := s.newTempGoEnv()
	if err != nil {
		return
	}
	defer func() {
		err2 := tempGoEnv.removeRef()
		if err2 != nil {
			if err == nil {
				err = err2
			} else {
				log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", tempGoEnv.TmpDir, err2)
			}
		}
	}()
	runCmdResource, err := s.runCmdResourcePool.acquire(ctx)
	if err != nil {
		return
	}
	defer runCmdResource.release()
	fSeeStorage, info2, _, err := s.getGoModuleAndIndexIfNeeded(ctx, tempGoEnv, moduleVersion, runCmdResource)
	if err != nil {
		return
	}
	if !fSeeStorage {
		info = info2
		return
	}
	moduleVersionCanonical := &module.Version{
		Path:    moduleVersion.Path,
		Version: info2.Version,
	}
	info, err = s.infoFromConcatObj(ctx, moduleVersionCanonical)
	if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
		return
	}
	info, err = s.infoFromGoModObj(ctx, moduleVersionCanonical)
	return
}

func (s *Service) infoFromConcatObj(ctx context.Context, moduleVersion *module.Version) (i *gomoduleservice.Info, err error) {
	data, err := s.storage.GetObject(ctx, storageConcatObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err == nil {
		defer func() {
			err2 := data.Close()
			if err2 != nil {
				if err == nil {
					err = err2
				} else {
					log.Errorf("error closing reader of concat obj:  %v", err)
				}
			}
		}()
		var commitTime time.Time
		commitTime, _, _, _, err = parseConcatObjCommon(data)
		if err != nil {
			err = fmt.Errorf("error parsing concat obj's data: %w", err)
			return
		}
		i = &gomoduleservice.Info{
			Time:    commitTime,
			Version: moduleVersion.Version,
		}
	} else {
		err = mapStorageError(err)
	}
	return
}

func (s *Service) infoFromGoModObj(ctx context.Context, moduleVersion *module.Version) (*gomoduleservice.Info, error) {
	metadata, err := s.storage.GetObjectMetadata(ctx, storageGoModObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err == nil {
		commitTimeStr := metadata[storageGoModObjCommitTimeMetadataKey]
		commitTime, err := time.Parse(time.RFC3339, commitTimeStr)
		if err != nil {
			return nil, fmt.Errorf("error parsing goMod obj's commit time metadata (%#v): %w", commitTimeStr, err)
		}
		return &gomoduleservice.Info{
			Time:    commitTime,
			Version: moduleVersion.Version,
		}, nil
	} else {
		err = mapStorageError(err)
	}
	return nil, err
}

func (s *Service) indexGoModule(tempGoEnv *tempGoEnv, commitTime time.Time, goModFD, zipFD *sharedFD, moduleVersion module.Version) {
	defer func() {
		err := goModFD.removeRef()
		if err != nil {
			log.Errorf("*sharedFD(%p, %#v) error closing fd: %v", goModFD, goModFD.FD.Name(), err)
		}
		err = zipFD.removeRef()
		if err != nil {
			log.Errorf("*sharedFD(%p, %#v) error closing fd: %v", zipFD, zipFD.FD.Name(), err)
		}
		err = tempGoEnv.removeRef()
		if err != nil {
			log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", tempGoEnv.TmpDir, err)
		}
	}()
	ctx := context.Background()
	err := fdSeekToStart(goModFD.FD)
	if err != nil {
		log.Errorf("indexGoModule failed: %v", err)
		return
	}
	name := storageGoModObjNamePrefix + moduleVersion.Path + "@" + moduleVersion.Version
	err = s.storage.CreateObjectExclusively(ctx, name,
		storage.ObjectMetadata{
			storageGoModObjCommitTimeMetadataKey: commitTime.UTC().Format(time.RFC3339),
		}, goModFD.FD)
	if err != nil {
		if !storage.ErrorIsCode(err, storage.PreconditionFailed) {
			log.Errorf("indexGoModule failed: %v", err)
			return
		}
	} else {
		log.Infof("stored object %#v", name)
	}
	err = fdSeekToStart(zipFD.FD)
	if err != nil {
		log.Errorf("indexGoModule failed: %v", err)
		return
	}
	name = storageZipObjNamePrefix + moduleVersion.Path + "@" + moduleVersion.Version
	err = s.storage.CreateObjectExclusively(ctx, name, nil, zipFD.FD)
	if err != nil {
		if !storage.ErrorIsCode(err, storage.PreconditionFailed) {
			log.Errorf("indexGoModule failed: %v", err)
			return
		}
	} else {
		log.Infof("stored object %#v", name)
	}
	name = storageConcatObjNamePrefix + moduleVersion.Path + "@" + moduleVersion.Version
	err = s.storage.DeleteObject(ctx, name)
	if err != nil {
		if !storage.ErrorIsCode(err, storage.NotFound) {
			log.Errorf("indexGoModule failed: %v", err)
			return
		}
	} else {
		log.Infof("deleted object %#v", name)
	}
}

func (s *Service) initializeTempGoEnvForModule(ctx context.Context, tempGoEnv *tempGoEnv, modulePath string) error {
	gitConfig := git.Config{}
	if privateModulesElement := s.getPrivateModulesElement(modulePath); privateModulesElement != nil {
		if privateModulesElement.Auth.GitHubApp != nil {
			// This indicates the module is private and hosted on github.com or another GitHub instance
			// ...so we configure git credential helper.
			gitConfig["credential"] = []git.KeyValuePair{
				{Key: "helper", Value: fmt.Sprintf("!%s --go-module-path=%s", s.gitCredentialHelperShell, shellescape.Quote(modulePath))},
				{Key: "useHttpPath", Value: "true"},
			}
		} else {
			// Return this error as a reminder
			return fmt.Errorf("TODO configure auth for private modules that do not use GitHub App credentials")
		}
		tempGoEnv.Environ.Set("GOPROXY", "direct")
		tempGoEnv.Environ.Set("GOSUMDB", "off")
	} else {
		tempGoEnv.Environ.Set("GOPROXY", s.envGoProxy)
		if s.publicModulesGoSumDBEnvVar != "" {
			tempGoEnv.Environ.Set("GOSUMDB", s.publicModulesGoSumDBEnvVar)
		} else {
			tempGoEnv.Environ.Unset("GOSUMDB")
		}
	}
	tempGoEnv.Environ.Set("no_proxy", s.httpProxyInfo.LibcurlNoProxy)
	tempGoEnv.Environ.Set("https_proxy", s.httpProxyInfo.LibcurlHTTPSProxy)
	err := git.WriteConfigFile(filepath.Join(tempGoEnv.HomeDir, ".gitconfig"), gitConfig)
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) Latest(ctx context.Context, modulePath string) (info *gomoduleservice.Info, err error) {
	err = s.notFoundOptimizations(modulePath)
	if err != nil {
		return
	}
	versionForGoCmd := "latest"
	if privateModulesElement := s.getPrivateModulesElement(modulePath); privateModulesElement == nil {
		// Optimization heuristic for public modules:
		// Get latest from parent proxy instead of doing the HTTP request via a Go command and return the version from the cache
		// if it is already cached. This does not work well if the latest version changes a lot, because then we waste more work
		// checking the cache than we gain.
		info, err = modproxyclient.Latest(ctx, s.parentProxyURL, s.httpClient, modulePath)
		if err != nil {
			if err == modproxyclient.ErrNotFound {
				err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, "%v", err)
			}
			return
		}
		if !s.readAfterListIsStronglyConsistent {
			return
		}
		log.Tracef("@latest for module %#v is %#v (from parent proxy), ensuring version is cached...", modulePath, info.Version)
		moduleVersion := &module.Version{
			Path:    modulePath,
			Version: info.Version,
		}
		info, err = s.infoFromConcatObj(ctx, moduleVersion)
		if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
			return
		}
		info, err = s.infoFromGoModObj(ctx, moduleVersion)
		if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
			return
		}
		versionForGoCmd = moduleVersion.Version
	}
	tempGoEnv, err := s.newTempGoEnv()
	if err != nil {
		return
	}
	defer func() {
		err2 := tempGoEnv.removeRef()
		if err2 != nil {
			if err == nil {
				err = err2
			} else {
				log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", tempGoEnv.TmpDir, err2)
			}
		}
	}()
	runCmdResource, err := s.runCmdResourcePool.acquire(ctx)
	if err != nil {
		return
	}
	defer runCmdResource.release()
	fSeeStorage, info2, _, err := s.getGoModuleAndIndexIfNeeded(ctx, tempGoEnv, &module.Version{
		Path:    modulePath,
		Version: versionForGoCmd,
	}, runCmdResource)
	if err != nil {
		return
	}
	if !fSeeStorage {
		info = info2
		return
	}
	moduleVersion := &module.Version{
		Path:    modulePath,
		Version: info2.Version,
	}
	info, err = s.infoFromConcatObj(ctx, moduleVersion)
	if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
		return
	}
	info, err = s.infoFromGoModObj(ctx, moduleVersion)
	return
}

func (s *Service) newTempGoEnv() (*tempGoEnv, error) {
	return newTempGoEnv(s.scratchDir, s.tempGoEnvBaseEnviron)
}

func (s *Service) notFoundOptimizations(modulePath string) (err error) {
	i := strings.IndexByte(modulePath, '/')
	var host string
	if i >= 0 {
		host = modulePath[:i]
	} else {
		host = modulePath
	}
	switch host {
	case "github.com":
		if i >= 0 {
			j := strings.IndexByte(modulePath[i+1:], '/') + i + 1
			if j > i {
				// Two or more slashes
				return
			}
			// One slash
		} // No slashes
		err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, `invalid github.com import path %#v`, modulePath)
		return
	}
	return
}

func (s *Service) versionPreamble(version string, allowNonCanonicalVersion bool) (fSeeStorage bool, err error) {
	if version == "" {
		err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, `module version "" is invalid`)
		return
	}
	if version == "latest" {
		err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, `module version "latest" is invalid`)
		return
	}
	if version != module.CanonicalVersion(version) {
		if !allowNonCanonicalVersion {
			err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, `module version %#v is invalid`, version)
		}
		return
	}
	fSeeStorage = true
	return
}

func (s *Service) Zip(ctx context.Context, moduleVersion *module.Version) (data io.ReadCloser, err error) {
	err = s.notFoundOptimizations(moduleVersion.Path)
	if err != nil {
		return
	}
	fSeeStorage, err := s.versionPreamble(moduleVersion.Version, false)
	if err != nil {
		return
	}
	if fSeeStorage {
		data, err = s.storage.GetObject(ctx, storageZipObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
		if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
			err = mapStorageError(err)
			return
		}
		data, err = s.zipFromConcatObj(ctx, moduleVersion)
		if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
			return
		}
	}
	tempGoEnv, err := s.newTempGoEnv()
	if err != nil {
		return
	}
	defer func() {
		err2 := tempGoEnv.removeRef()
		if err2 != nil {
			if err == nil {
				err = err2
			} else {
				log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", tempGoEnv.TmpDir, err2)
			}
		}
	}()
	runCmdResource, err := s.runCmdResourcePool.acquire(ctx)
	if err != nil {
		return
	}
	defer runCmdResource.release()
	fSeeStorage, info, downloadInfo, err := s.getGoModuleAndIndexIfNeeded(ctx, tempGoEnv, moduleVersion, runCmdResource)
	if err != nil {
		return
	}
	if !fSeeStorage {
		data, err = tempGoEnv.open(downloadInfo.Zip)
		return
	}
	data, err = s.zipFromConcatObj(ctx, &module.Version{
		Path:    moduleVersion.Path,
		Version: info.Version,
	})
	if err == nil || !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
		return
	}
	data, err = s.storage.GetObject(ctx, storageZipObjNamePrefix+moduleVersion.Path+"@"+info.Version)
	err = mapStorageError(err)
	return
}

func (s *Service) zipFromConcatObj(ctx context.Context, moduleVersion *module.Version) (d io.ReadCloser, err error) {
	didPanic := true
	data, err := s.storage.GetObject(ctx, storageConcatObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err != nil {
		err = mapStorageError(err)
		return
	}
	defer func() {
		if didPanic || err != nil {
			err2 := data.Close()
			if err2 != nil {
				log.Errorf("error closing %T: %v", data, err2)
			}
		}
	}()
	_, _, goModToRead, zipPrefix, err := parseConcatObjCommon(data)
	if err != nil {
		err = fmt.Errorf("error parsing concat obj's data: %w", err)
		didPanic = false
		return
	}
	if goModToRead > 0 {
		_, err = io.CopyN(ioutil.Discard, data, int64(goModToRead))
		if err != nil {
			didPanic = false
			return
		}
		d = data
		didPanic = false
		return
	}
	d = util.NewConcatReader(zipPrefix, data, nil, data.Close)
	didPanic = false
	return
}
