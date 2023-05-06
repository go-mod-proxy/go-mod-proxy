package gocmd

import (
	"os"
	"path/filepath"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
)

type sharedFD struct {
	basename string
	FD       *os.File
	refs     int32
}

func newSharedFDOpen(name string) (*sharedFD, error) {
	fd, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	s := &sharedFD{
		basename: filepath.Base(name),
		FD:       fd,
		refs:     1,
	}
	log.Tracef("*sharedFD(%p, %#v) initialized refs=1", s, s.basename)
	return s, nil
}

func (s *sharedFD) addRef() {
	new := atomic.AddInt32(&s.refs, 1)
	log.Tracef("*sharedFD(%p, %#v) refs=%d", s, s.basename, new)
}

func (s *sharedFD) removeRef() error {
	new := atomic.AddInt32(&s.refs, -1)
	log.Tracef("*sharedFD(%p, %#v) refs=%d", s, s.basename, new)
	if new == 0 {
		err := s.FD.Close()
		if err != nil {
			return err
		}
		log.Tracef("*sharedFD(%p, %#v) closed fd", s, s.basename)
	}
	return nil
}
