package gocmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	log "github.com/sirupsen/logrus"

	"github.com/go-mod-proxy/go-mod-proxy/internal/util"
)

func getTempGoEnvBaseEnviron() *util.Environ {
	isWindows := runtime.GOOS == "windows"
	isCaseSensitive := !isWindows
	// Instead of copying all environment variables and then overriding variables,
	// we copy only specific environment variables named (those named in whitelist).
	// The latter provides stronger isolation (i.e. more reproducible results and less
	// security considerations).
	whitelist := map[string]struct{}{
		"PATH": {},
	}
	if isWindows {
		whitelist["SYSTEMROOT"] = struct{}{}
	}
	environ := util.NewEnviron(nil, isCaseSensitive)
	for name := range whitelist {
		if value, ok := os.LookupEnv(name); ok {
			environ.Set(name, value)
		}
	}
	environ.Set("CGO_ENABLED", "0")
	environ.Set("GIT_ALLOW_PROTOCOL", "git:https")
	// We set GIT_CONFIG_NOSYSTEM to have less interference of environment.
	environ.Set("GIT_CONFIG_NOSYSTEM", "1")
	environ.Set("GIT_TERMINAL_PROMPT", "0")
	environ.Set("GO111MODULE", "on")
	environ.Set("GOFLAGS", "-mod=mod")
	return environ
}

type tempGoEnv struct {
	Environ    *util.Environ
	GoPathDir  string
	GoCacheDir string
	HomeDir    string
	TmpDir     string
	refs       int32
	WorkDir    string
}

func newTempGoEnv(scratchDir string, baseEnviron *util.Environ) (t2 *tempGoEnv, err error) {
	t := &tempGoEnv{}
	t.TmpDir, err = os.MkdirTemp(scratchDir, "")
	if err != nil {
		return
	}
	didPanic := true
	defer func() {
		if didPanic || err != nil {
			err2 := t.removeTmpDir()
			if err2 != nil {
				log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", t.TmpDir, err2)
			}
		}
	}()
	log.Tracef("created tmpDir %#v for *tempGoEnv", t.TmpDir)
	t.GoPathDir = filepath.Join(t.TmpDir, "gopath")
	t.GoCacheDir = filepath.Join(t.TmpDir, "gocache")
	t.HomeDir = filepath.Join(t.TmpDir, "home")
	t.WorkDir = filepath.Join(t.HomeDir, "work")
	err = os.Mkdir(t.GoPathDir, 0700)
	if err != nil {
		didPanic = false
		return
	}
	err = os.Mkdir(t.GoCacheDir, 0700)
	if err != nil {
		didPanic = false
		return
	}
	err = os.MkdirAll(t.WorkDir, 0700)
	if err != nil {
		didPanic = false
		return
	}
	environ := baseEnviron.Copy()
	// Set PWD as optimization for os.Getwd()
	environ.Set("PWD", t.WorkDir)
	environ.Set("HOME", t.HomeDir)
	environ.Set("GOPATH", t.GoPathDir)
	environ.Set("GOCACHE", t.GoCacheDir)
	// Set XDG_CONFIG_HOME to disable various defaults in git
	environ.Set("XDG_CONFIG_HOME", filepath.Join(t.TmpDir, "non-existing"))
	t.Environ = environ
	t.addRef()
	runtime.SetFinalizer(t, (*tempGoEnv).removeTmpDirLogError)
	t2 = t
	didPanic = false
	return
}

func (t *tempGoEnv) addRef() {
	atomic.AddInt32(&t.refs, 1)
}

func (t *tempGoEnv) open(name string) (*tempGoEnvFD, error) {
	if !strings.HasPrefix(name, t.TmpDir) || !filepath.IsAbs(name) {
		return nil, fmt.Errorf("name is invalid")
	}
	if atomic.LoadInt32(&t.refs) <= 0 {
		panic(fmt.Errorf("attempt to use *tempGoEnv who's TmpDir may already have been removed"))
	}
	fd, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return newTempGoEnvFD(fd, name, t)
}

func (t *tempGoEnv) removeRef() error {
	if atomic.AddInt32(&t.refs, -1) == 0 {
		runtime.SetFinalizer(t, nil)
		return t.removeTmpDir()
	}
	return nil
}

func (t *tempGoEnv) removeTmpDir() error {
	err := filepath.Walk(t.TmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(path, 0700)
	})
	err2 := os.RemoveAll(t.TmpDir)
	if err2 != nil {
		if err == nil {
			err = err2
		} else {
			log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", t.TmpDir, err2)
		}
	} else {
		log.Tracef("removed tmpDir %#v of *tempGoEnv", t.TmpDir)
	}
	return err
}

func (t *tempGoEnv) removeTmpDirLogError() {
	err := t.removeTmpDir()
	if err != nil {
		log.Errorf("error removing tmpDir %#v of *tempGoEnv: %v", t.TmpDir, err)
	}
}

type tempGoEnvFD struct {
	closed bool
	FD     *os.File
	Name   string
	t      *tempGoEnv
}

var _ io.Closer = (*tempGoEnvFD)(nil)
var _ io.ReadSeeker = (*tempGoEnvFD)(nil)

func newTempGoEnvFD(fd *os.File, name string, t *tempGoEnv) (*tempGoEnvFD, error) {
	if fd == nil {
		return nil, fmt.Errorf("fd must not be nil")
	}
	tt := &tempGoEnvFD{
		FD:   fd,
		Name: name,
		t:    t,
	}
	runtime.SetFinalizer(tt, (*tempGoEnvFD).finalize)
	t.addRef()
	return tt, nil
}

func (t *tempGoEnvFD) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	runtime.SetFinalizer(t, nil)
	return t.closeCore()
}

func (t *tempGoEnvFD) closeCore() error {
	err := t.FD.Close()
	if err != nil {
		return err
	}
	return t.t.removeRef()
}

func (t *tempGoEnvFD) finalize() {
	err := t.closeCore()
	if err != nil {
		log.Errorf("got unexpected error from (*tempGoEnvFD).closeCore: %v", err)
	}
}

func (t *tempGoEnvFD) Read(p []byte) (n int, err error) {
	n, err = t.FD.Read(p)
	return
}

func (t *tempGoEnvFD) Seek(offset int64, whence int) (int64, error) {
	return t.FD.Seek(offset, whence)
}
