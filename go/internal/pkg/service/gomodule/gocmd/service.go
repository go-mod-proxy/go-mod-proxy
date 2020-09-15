package gocmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alessio/shellescape"
	log "github.com/sirupsen/logrus"
	module "golang.org/x/mod/module"

	"github.com/jbrekelmans/go-module-proxy/internal/pkg/config"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/git"
	gomoduleservice "github.com/jbrekelmans/go-module-proxy/internal/pkg/service/gomodule"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/service/storage"
	"github.com/jbrekelmans/go-module-proxy/internal/pkg/util"
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
	// Info is the path to a file containing JSON marshalling of a "github.com/jbrekelmans/go-module-proxy/internal/app/service/gomodule".Info value.
	Info  string
	GoMod string
	// Zip is the path to a file containing Zip file
	Zip      string
	Dir      string
	Sum      string
	Time     time.Time
	GoModSum string
}

type ServiceOptions struct {
	GitCredentialHelperShell string
	HTTPProxyInfo            *config.HTTPProxyInfo
	MaxParallelCommands      int
	ParentProxy              *url.URL
	PrivateModules           []*config.PrivateModulesElement
	ScratchDir               string
	Storage                  storage.Storage
}

type Service struct {
	envGoProxy               string
	gitCredentialHelperShell string
	goBinFile                string
	httpProxyInfo            *config.HTTPProxyInfo
	privateModules           []*config.PrivateModulesElement
	runCmdResourcePool       *maxParallelismResourcePool
	scratchDir               string
	storage                  storage.Storage
	tempGoEnvBaseEnviron     *util.Environ
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
	ss := &Service{
		gitCredentialHelperShell: opts.GitCredentialHelperShell,
		goBinFile:                goBinFile2,
		httpProxyInfo:            opts.HTTPProxyInfo,
		privateModules:           opts.PrivateModules,
		scratchDir:               scratchDir2,
		runCmdResourcePool:       runCmdResourcePool,
		storage:                  opts.Storage,
		tempGoEnvBaseEnviron:     getTempGoEnvBaseEnviron(),
	}
	if opts.ParentProxy != nil {
		parentProxyStr := opts.ParentProxy.String()
		// , is valid in URLs, but illegal in GOPROXY environment variable
		if strings.ContainsAny(parentProxyStr, ",") {
			return nil, fmt.Errorf(`opts.ParentProxy is invalid because opts.ParentProxy.String() contains illegal character ","`)
		}
		ss.envGoProxy = parentProxyStr + ",direct"
	}
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

	// if strings.Contains(downloadInfo.Error.Err, "unknown revision") {
	// 	log.Tracef("NOT FOUND")
	// 	err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, "command %s succeeded but got unexpected error loading module:\n%s",
	// 		formatArgs(args), strLog)
	// 	return
	// }

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
	if moduleVersion.Version == "latest" {
		if info.Version == "latest" {
			err = fmt.Errorf(".info file created by %s command has unexpected version %#v", formatArgs(args), info.Version)
			return
		}
	} else if info.Version != moduleVersion.Version {
		err = fmt.Errorf(".info file created by %s command has version %#v that is unexpectedly inconsistent with the requested version",
			formatArgs(args), info.Version)
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

func (s *Service) GoMod(ctx context.Context, moduleVersion *module.Version) (data io.ReadCloser, err error) {
	if moduleVersion.Version == "latest" {
		err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, `module version "latest" is invalid`)
		return
	}
	data, err = s.storage.GetObject(ctx, storageGoModObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
	}
	data, err = s.goModFromConcatObj(ctx, moduleVersion)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
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
	fSeeStorage, _, downloadInfo, err := s.getGoModuleAndIndexIfNeeded(ctx, tempGoEnv, moduleVersion, runCmdResource)
	if err != nil {
		return
	}
	if !fSeeStorage {
		data, err = tempGoEnv.open(downloadInfo.GoMod)
		return
	}
	data, err = s.goModFromConcatObj(ctx, moduleVersion)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
	}
	data, err = s.storage.GetObject(ctx, storageGoModObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	return
}

func (s *Service) goModFromConcatObj(ctx context.Context, moduleVersion *module.Version) (d io.ReadCloser, err error) {
	data, err := s.storage.GetObject(ctx, storageConcatObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err != nil {
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
	if moduleVersion.Version == "latest" {
		err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, `module version "latest" is invalid`)
		return
	}
	info, err = s.infoFromGoModObj(ctx, moduleVersion)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
	}
	info, err = s.infoFromConcatObj(ctx, moduleVersion)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
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
	info, err = s.infoFromConcatObj(ctx, moduleVersion)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
	}
	info, err = s.infoFromGoModObj(ctx, moduleVersion)
	return
}

func (s *Service) infoFromConcatObj(ctx context.Context, moduleVersion *module.Version) (*gomoduleservice.Info, error) {
	data, err := s.storage.GetObject(ctx, storageConcatObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err == nil {
		commitTime, _, _, _, err := parseConcatObjCommon(data)
		if err != nil {
			return nil, fmt.Errorf("error parsing concat obj's data: %w", err)
		}
		return &gomoduleservice.Info{
			Time:    commitTime,
			Version: moduleVersion.Version,
		}, nil
	}
	return nil, err
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
	var privateModulesElement2 *config.PrivateModulesElement
	for _, privateModulesElement1 := range s.privateModules {
		if util.PathIsLexicalDescendant(modulePath, privateModulesElement1.PathPrefix) {
			privateModulesElement2 = privateModulesElement1
			break
		}
	}
	gitConfig := git.Config{}
	if privateModulesElement2 != nil {
		if privateModulesElement2.Auth.GitHubApp != nil {
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
		if s.envGoProxy != "" {
			tempGoEnv.Environ.Set("GOPROXY", s.envGoProxy)
		} else {
			// Use default proxy
			tempGoEnv.Environ.Unset("GOPROXY")
		}
		// Use default sum database
		tempGoEnv.Environ.Unset("GOSUMDB")
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
		Version: "latest",
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
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
	}
	info, err = s.infoFromGoModObj(ctx, moduleVersion)
	return
}

func (s *Service) List(ctx context.Context, modulePath string) (d io.ReadCloser, err error) {
	versionMap := map[string]struct{}{}
	err = s.listAddObjectNames(ctx, storageGoModObjNamePrefix+modulePath+"@", versionMap)
	if err != nil {
		return
	}
	err = s.listAddObjectNames(ctx, storageConcatObjNamePrefix+modulePath+"@", versionMap)
	if err != nil {
		return
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
	err = s.initializeTempGoEnvForModule(ctx, tempGoEnv, modulePath)
	if err != nil {
		return
	}
	args := []string{s.goBinFile, "list", "-m", "-versions", "-json", modulePath}
	stdout, strLog, err := s.runCmd(ctx, tempGoEnv, args)
	if err != nil {
		return
	}
	runCmdResource.release()
	var goListInfo goModuleInfo
	err = util.UnmarshalJSON(bytes.NewReader(stdout), &goListInfo, true)
	if err != nil {
		err = fmt.Errorf("command %s succeeded but got unexpected stderr/stdout:\n%s", formatArgs(args), strLog)
		return
	}
	var goListVersions []string
	if len(goListInfo.Versions) == 0 {
		if goListInfo.Version == "" {
			err = fmt.Errorf("command %s succeeded but got unexpected stderr/stdout:\n%s", formatArgs(args), strLog)
			return
		}
		goListVersions = []string{goListInfo.Version}
	} else {
		goListVersions = goListInfo.Versions
	}
	var waitGroup sync.WaitGroup
	var versionMapMutex sync.Mutex
	for _, version := range goListVersions {
		versionMapMutex.Lock()
		_, flag := versionMap[version]
		versionMapMutex.Unlock()
		if flag {
			continue
		}
		version := version
		var runCmdResource maxParallelismResource
		runCmdResource, err = s.runCmdResourcePool.acquire(ctx)
		if err != nil {
			return
		}
		waitGroup.Add(1)
		go func() {
			defer runCmdResource.release()
			defer waitGroup.Done()
			tempGoEnv, err := s.newTempGoEnv()
			if err != nil {
				log.Error(err)
				return
			}
			defer func() {
				err := tempGoEnv.removeRef()
				if err != nil {
					log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", tempGoEnv.TmpDir, err)
				}
			}()
			_, _, _, err = s.getGoModuleAndIndexIfNeeded(ctx, tempGoEnv, &module.Version{Path: modulePath, Version: version}, runCmdResource)
			if err != nil {
				log.Error(err)
				return
			}
			versionMapMutex.Lock()
			versionMap[version] = struct{}{}
			versionMapMutex.Unlock()
		}()
	}
	waitGroup.Wait()
	var sb strings.Builder
	for version := range versionMap {
		// TODO filter versions...
		sb.WriteString(version)
		sb.WriteByte('\n')
	}
	d = ioutil.NopCloser(strings.NewReader(sb.String()))
	return
}

func (s *Service) listAddObjectNames(ctx context.Context, namePrefix string, versionMap map[string]struct{}) (err error) {
	var pageToken string
	for {
		var objList *storage.ObjectList
		objList, err = s.storage.ListObjects(ctx, storage.ObjectListOptions{
			NamePrefix: namePrefix,
			PageToken:  pageToken,
		})
		if err != nil {
			return
		}
		for _, name := range objList.Names {
			versionMap[name[len(namePrefix):]] = struct{}{}
		}
		pageToken = objList.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return
}

func (s *Service) newTempGoEnv() (*tempGoEnv, error) {
	return newTempGoEnv(s.scratchDir, s.tempGoEnvBaseEnviron)
}

func (s *Service) Zip(ctx context.Context, moduleVersion *module.Version) (data io.ReadCloser, err error) {
	if moduleVersion.Version == "latest" {
		err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, `module version "latest" is invalid`)
		return
	}
	data, err = s.storage.GetObject(ctx, storageZipObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
	}
	data, err = s.zipFromConcatObj(ctx, moduleVersion)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
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
	fSeeStorage, _, downloadInfo, err := s.getGoModuleAndIndexIfNeeded(ctx, tempGoEnv, moduleVersion, runCmdResource)
	if err != nil {
		return
	}
	if !fSeeStorage {
		data, err = tempGoEnv.open(downloadInfo.Zip)
		return
	}
	data, err = s.zipFromConcatObj(ctx, moduleVersion)
	if err == nil || !storage.ErrorIsCode(err, storage.NotFound) {
		return
	}
	data, err = s.storage.GetObject(ctx, storageZipObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	return
}

func (s *Service) zipFromConcatObj(ctx context.Context, moduleVersion *module.Version) (d io.ReadCloser, err error) {
	didPanic := true
	data, err := s.storage.GetObject(ctx, storageConcatObjNamePrefix+moduleVersion.Path+"@"+moduleVersion.Version)
	if err != nil {
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
