package gocmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"golang.org/x/mod/module"

	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/modproxyclient"
	gomoduleservice "github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/service/gomodule"
	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/service/storage"
	"github.com/go-mod-proxy/go-mod-proxy/go/internal/pkg/util"
)

func (s *Service) List(ctx context.Context, modulePath string) (io.ReadCloser, error) {
	if err := s.notFoundOptimizations(modulePath); err != nil {
		return nil, err
	}
	privateModulesElement := s.getPrivateModulesElement(modulePath)
	if privateModulesElement == nil && !s.readAfterListIsStronglyConsistent {
		goListVersions, err := s.listViaParentProxy(ctx, modulePath)
		if err != nil {
			return nil, err
		}
		var sb strings.Builder
		for _, version := range goListVersions {
			sb.WriteString(version)
			sb.WriteByte('\n')
		}
		return ioutil.NopCloser(strings.NewReader(sb.String())), nil
	}
	versionMap := map[string]struct{}{}
	versionMapMutex := new(sync.Mutex)
	const errChanSize = 3
	// The constant 3 must be aligned with the number of Goroutines started (before reading from errChan),
	// and each such Goroutine  must send on errChan exactly once.
	errChan := make(chan error, errChanSize)
	ctx, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()
	go s.listAddObjectNames(ctx, errChan, StorageGoModObjNamePrefix+modulePath+"@", versionMap, versionMapMutex)
	go s.listAddObjectNames(ctx, errChan, StorageConcatObjNamePrefix+modulePath+"@", versionMap, versionMapMutex)
	var goListVersions []string
	go func() {
		var err error
		if privateModulesElement == nil {
			goListVersions, err = s.listViaParentProxy(ctx, modulePath)
		} else {
			goListVersions, err = s.listGoCmd(ctx, modulePath)
		}
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
	if !s.readAfterListIsStronglyConsistent {
		for _, version := range goListVersions {
			versionMap[version] = struct{}{}
		}
	} else {
		var waitGroup sync.WaitGroup
		for _, version := range moveSliceElementsThatAreInMapToBack(goListVersions, versionMap) {
			version := version
			runCmdResource, err := s.runCmdResourcePool.acquire(ctx)
			if err != nil {
				return nil, err
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
	}
	var sb strings.Builder
	for version := range versionMap {
		sb.WriteString(version)
		sb.WriteByte('\n')
	}
	return ioutil.NopCloser(strings.NewReader(sb.String())), nil
}

func (s *Service) listAddObjectNames(ctx context.Context, errChan chan<- error, namePrefix string,
	versionMap map[string]struct{}, versionMapMutex *sync.Mutex) {
	var pageToken string
	for {
		objList, err := s.storage.ListObjects(ctx, storage.ObjectListOptions{
			NamePrefix: namePrefix,
			PageToken:  pageToken,
		})
		if err != nil {
			errChan <- mapStorageError(err)
			return
		}
		versionMapMutex.Lock()
		for _, obj := range objList.Objects {
			version := obj.Name[len(namePrefix):]
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
	if !gomoduleservice.ErrorIsCode(err, gomoduleservice.NotFound) {
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
	if err == modproxyclient.ErrNotFound {
		err = gomoduleservice.NewErrorf(gomoduleservice.NotFound, "%v", err)
	}
	return
}

// moveSliceElementsThatAreInMapToBack swaps elements in s so that all elements in m are in a continuous subslice ending at the end of s.
// The relative ordering of elements is not preserved.
// moveSliceElementsThatAreInMapToBack returns a subslice of s so that no elemenets in the subslice are in m.
func moveSliceElementsThatAreInMapToBack(s []string, m map[string]struct{}) []string {
	i := 0
	end := len(s)
	for i < end {
		if _, ok := m[s[i]]; ok {
			swap(s, i, end-1)
			end--
		} else {
			i++
		}
	}
	return s[:end]
}

func swap(s []string, i, j int) {
	t := s[i]
	s[i] = s[j]
	s[j] = t
}
