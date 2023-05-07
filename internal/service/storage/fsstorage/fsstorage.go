package fs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"

	internalErrors "github.com/go-mod-proxy/go-mod-proxy/internal/errors"
	"github.com/go-mod-proxy/go-mod-proxy/internal/service/storage"
)

type FSStorage struct {
	root string
}

var _ storage.Storage = (*FSStorage)(nil)

func NewFSStorage(root string) (*FSStorage, error) {
	f := &FSStorage{
		root: root,
	}
	return f, nil
}

func (f *FSStorage) CreateObjectExclusively(ctx context.Context,
	name string, objectMetadata storage.ObjectMetadata,
	data io.ReadSeeker) (err error) {
	dir, base, err := f.getDirAndBase(name)
	if err != nil {
		return
	}
	dataFD, dataFDClose := createTemp(dir, base+".*"+dataSuffix, &err)
	if err != nil {
		return
	}
	defer func() {
		dataFDClose()
		if err != nil {
			if removeErr := os.Remove(dataFD.Name()); removeErr != nil {
				log.Errorf("error unlinking file %#v: %v", dataFD.Name(), removeErr)
			}
		}
	}()
	if _, err = io.Copy(dataFD, data); err != nil {
		return
	}
	dataFDClose()
	if err != nil {
		return
	}
	jsonFD, jsonFDClose := createTemp(dir, base+".*"+jsonSuffix, &err)
	if err != nil {
		return
	}
	defer func() {
		jsonFDClose()
		if err != nil {
			if removeErr := os.Remove(jsonFD.Name()); removeErr != nil {
				log.Errorf("error unlinking file %#v: %v", jsonFD.Name(), removeErr)
			}
		}
	}()
	if err = json.NewEncoder(jsonFD).Encode(jsonData{
		D: filepath.Base(dataFD.Name()),
		M: objectMetadata,
	}); err != nil {
		return
	}
	jsonFDClose()
	if err != nil {
		return
	}
	linkFile := filepath.Join(dir, base+linkSuffix)
	err = os.Symlink(filepath.Base(jsonFD.Name()), linkFile)
	if errors.Is(err, os.ErrExist) {
		err = internalErrors.NewError(internalErrors.PreconditionFailed, err.Error())
	}
	return
}

func (f *FSStorage) DeleteObject(ctx context.Context, name string) (err error) {
	dir, base, err := f.getDirAndBase(name)
	if err != nil {
		return
	}
	linkFile := filepath.Join(dir, base+linkSuffix)
	jsonFileRelative, err := os.Readlink(linkFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = internalErrors.NewError(internalErrors.NotFound, err.Error())
		}
		return
	}
	unlink := func(file string) {
		if removeErr := os.Remove(file); removeErr != nil {
			if err == nil {
				err = removeErr
			} else {
				log.Errorf("error unlinking file %#v: %v", file, removeErr)
			}
		}
	}
	jsonFile := filepath.Join(dir, jsonFileRelative)
	jsonFD, err := os.Open(jsonFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return
		}
	} else {
		defer func() {
			if closeErr := jsonFD.Close(); closeErr != nil {
				log.Errorf("error closing file %#v: %v", jsonFile, closeErr)
			}
		}()
		var jsonBytes []byte
		jsonBytes, err = io.ReadAll(jsonFD)
		if err != nil {
			return
		}
		defer unlink(jsonFile)
		var x jsonData
		if err = json.Unmarshal(jsonBytes, &x); err == nil {
			dataFile := filepath.Join(dir, x.D)
			unlink(dataFile)
		}
	}
	unlink(linkFile)
	return
}

func (f *FSStorage) getDirAndBase(name string) (dir, base string, err error) {
	if err = validateName(name); err != nil {
		return
	}
	filePrefix := filepath.Join(f.root, filepath.FromSlash(name))
	dir = filepath.Dir(filePrefix)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return
	}
	base = filepath.Base(filePrefix)
	return
}

func (f *FSStorage) GetObject(ctx context.Context, name string) (io.ReadCloser, error) {
	dir, base, err := f.getDirAndBase(name)
	if err != nil {
		return nil, err
	}
	linkFile := filepath.Join(dir, base+linkSuffix)
	jsonFD, err := os.Open(linkFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = internalErrors.NewError(internalErrors.NotFound, err.Error())
		}
		return nil, err
	}
	defer func() {
		if closeErr := jsonFD.Close(); closeErr != nil {
			log.Errorf("error closing file %#v: %v", linkFile, closeErr)
		}
	}()
	var x jsonData
	err = json.NewDecoder(jsonFD).Decode(&x)
	if err != nil {
		return nil, err
	}
	dataFile := filepath.Join(dir, x.D)
	return os.Open(dataFile)
}

func (f *FSStorage) GetObjectMetadata(ctx context.Context, name string) (storage.ObjectMetadata, error) {
	dir, base, err := f.getDirAndBase(name)
	if err != nil {
		return nil, err
	}
	linkFile := filepath.Join(dir, base+linkSuffix)
	jsonFD, err := os.Open(linkFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = internalErrors.NewError(internalErrors.NotFound, err.Error())
		}
		return nil, err
	}
	defer func() {
		if closeErr := jsonFD.Close(); closeErr != nil {
			log.Errorf("error closing file %#v: %v", linkFile, closeErr)
		}
	}()
	var x jsonData
	err = json.NewDecoder(jsonFD).Decode(&x)
	if err != nil {
		return nil, err
	}
	return x.M, nil
}

func (f *FSStorage) ListObjects(ctx context.Context, opts storage.ObjectListOptions) (*storage.ObjectList, error) {
	if opts.MaxResults < 0 {
		return nil, fmt.Errorf("opts.MaxResults is negative")
	}
	if opts.PageToken != "" {
		return nil, fmt.Errorf("non-empty opts.PageToken is not supported")
	}
	var objectNames []string
	dirCleaned := filepath.Clean(f.root)
	// TODO are there edge-cases where finding objectName from path does not work?
	if err := filepath.Walk(dirCleaned, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		pathWithoutSuffix, ok := strings.CutSuffix(path, linkSuffix)
		if !ok {
			return nil
		}
		var objectName string
		if dirCleaned == "." {
			objectName = pathWithoutSuffix
		} else {
			objectName = pathWithoutSuffix[len(dirCleaned)+1:]
		}
		if strings.HasPrefix(objectName, opts.NamePrefix) {
			objectNames = append(objectNames, objectName)
		}
		return nil
	}); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &storage.ObjectList{}, nil
		}
		return nil, err
	}
	sort.Strings(objectNames)
	if opts.MaxResults > 0 {
		objectNames = objectNames[:opts.MaxResults]
	}
	return &storage.ObjectList{
		Names: objectNames,
	}, nil
}

func createTemp(dir, pattern string, errPtr *error) (fd *os.File, closeOnce func()) {
	if errPtr == nil {
		panic(errors.New("errPtr is nil"))
	}
	fd, *errPtr = os.CreateTemp(dir, pattern)
	if *errPtr != nil {
		return
	}
	closeAttempted := false
	closeOnce = func() {
		if closeAttempted {
			return
		}
		closeAttempted = true
		err := fd.Close()
		if err == nil {
			return
		}
		if *errPtr == nil {
			*errPtr = err
		} else {
			log.Errorf("error closing file %#v: %v", fd.Name(), err)
		}
	}
	return
}

type jsonData struct {
	D string
	M storage.ObjectMetadata
}

const (
	// linkSuffix is the extension of symlinks to files storing object metadata.
	linkSuffix = ".link"
	// jsonSuffix is the extension of files storing object metadata and the name of the sibling
	// file that stores the object content.
	jsonSuffix = ".json"
	// dataSuffix is the extension of files storing object content.
	dataSuffix = ".data"
)
