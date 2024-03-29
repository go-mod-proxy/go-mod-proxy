package gocmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	module "golang.org/x/mod/module"

	internalErrors "github.com/go-mod-proxy/go-mod-proxy/internal/errors"
	"github.com/go-mod-proxy/go-mod-proxy/internal/modproxyclient"
	"github.com/go-mod-proxy/go-mod-proxy/internal/service/storage"
	"github.com/go-mod-proxy/go-mod-proxy/internal/util"
)

func (s *Service) List(ctx context.Context, modulePath string) (io.ReadCloser, error) {
	if err := s.notFoundOptimizations(modulePath); err != nil {
		return nil, err
	}
	privateModulesElement := s.getPrivateModulesElement(modulePath)
	if privateModulesElement == nil {
		goListVersions, err := s.listViaParentProxy(ctx, modulePath)
		if err != nil {
			return nil, err
		}
		var sb strings.Builder
		for _, version := range goListVersions {
			sb.WriteString(version)
			sb.WriteByte('\n')
		}
		return io.NopCloser(strings.NewReader(sb.String())), nil
	}
	versionMap := map[string]struct{}{}
	versionMapMutex := new(sync.Mutex)
	const errChanSize = 3
	// The constant 3 must be aligned with the number of Goroutines started (before reading from errChan),
	// and each such Goroutine  must send on errChan exactly once.
	errChan := make(chan error, errChanSize)
	ctx, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()
	go s.listAddObjectNames(ctx, errChan, storageGoModObjNamePrefix+modulePath+"@", versionMap, versionMapMutex)
	go s.listAddObjectNames(ctx, errChan, storageConcatObjNamePrefix+modulePath+"@", versionMap, versionMapMutex)
	var goListVersions []string
	go func() {
		var err error
		goListVersions, err = s.listGoCmd(ctx, modulePath)
		errChan <- err
	}()
	var err error
	for i := 0; i < errChanSize; i++ {
		err2 := <-errChan
		if err2 != nil {
			if err == nil {
				err = err2
				cancelFunc()
			} else {
				select {
				case <-ctx.Done():
					if err == ctx.Err() {
						continue
					}
				default:
				}
				log.Errorf("got secondary error while listing versions: %v", err)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	for _, version := range goListVersions {
		versionMap[version] = struct{}{}
	}
	var sb strings.Builder
	for version := range versionMap {
		sb.WriteString(version)
		sb.WriteByte('\n')
	}
	return io.NopCloser(strings.NewReader(sb.String())), nil
}

func (s *Service) listAddObjectNames(ctx context.Context, errChan chan<- error, namePrefix string,
	versionMap map[string]struct{}, versionMapMutex *sync.Mutex) {
	var pageToken string
	for {
		var objList *storage.ObjectList
		objList, err := s.storage.ListObjects(ctx, storage.ObjectListOptions{
			NamePrefix: namePrefix,
			PageToken:  pageToken,
		})
		if err != nil {
			errChan <- err
			return
		}
		versionMapMutex.Lock()
		for _, name := range objList.Names {
			version := name[len(namePrefix):]
			if !module.IsPseudoVersion(version) {
				versionMap[version] = struct{}{}
			}
		}
		versionMapMutex.Unlock()
		pageToken = objList.NextPageToken
		if pageToken == "" {
			break
		}
	}
	errChan <- nil
}

func (s *Service) listGoCmd(ctx context.Context, modulePath string) (goListVersions []string, err error) {
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
	goListVersions = goListInfo.Versions
	if len(goListInfo.Versions) == 0 {
		// NOTE: if len(goListInfo.Versions) == 0 then we will have actually queried the latest version of the module (if the command exited
		// with code 0). Use this information and index the version.
		go s.listGoCmdIndex(&module.Version{
			Path:    modulePath,
			Version: goListInfo.Version,
		})
	}
	return
}

func (s *Service) listGoCmdIndex(moduleVersion *module.Version) {
	log.Tracef(`discovered latest version (%#v) of module %#v through a \"go list\" command, eagerly caching this module version...`,
		moduleVersion.Version, moduleVersion.Path)
	ctx := context.Background()
	_, err := s.infoFromGoModObj(ctx, moduleVersion)
	if err == nil {
		log.Tracef(`latest version (%#v) of module %#v discovered through a \"go list\" command is already cached`,
			moduleVersion.Version, moduleVersion.Path)
		return
	}
	if !internalErrors.ErrorIsCode(err, internalErrors.NotFound) {
		log.Errorf(`error checking if latest version (%#v) of module %#v discovered through a \"go list\" command is already cached: %v`,
			moduleVersion.Version, moduleVersion.Path, err)
	}
	runCmdResource, err := s.runCmdResourcePool.acquire(ctx)
	if err != nil {
		panic(err)
	}
	defer runCmdResource.release()
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
	_, _, _, err = s.getGoModuleAndIndexIfNeeded(ctx, tempGoEnv, moduleVersion, runCmdResource)
	if err != nil {
		log.Errorf(`error caching latest version (%#v) of module %#v discovered through a \"go list\" command: %v`,
			moduleVersion.Version, moduleVersion.Path, err)
	}
}

func (s *Service) listViaParentProxy(ctx context.Context, modulePath string) (goListVersions []string, err error) {
	// For public modules send a request to the parent proxy directly instead of via a "go list" command.
	// This also avoids an unnecessary second request performed by a "go list" command when GET "/@v/list" returns zero versions.
	goListVersions, err = modproxyclient.List(ctx, s.parentProxyURL, s.httpClient, modulePath)
	return
}
