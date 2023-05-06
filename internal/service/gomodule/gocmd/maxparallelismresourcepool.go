package gocmd

import (
	"context"
	"fmt"
)

type maxParallelismResource func()

func (m maxParallelismResource) release() {
	m()
}

type maxParallelismResourcePool struct {
	c chan struct{}
}

func newMaxParallelismResourcePool(max int) (*maxParallelismResourcePool, error) {
	if max <= 0 {
		return nil, fmt.Errorf("max must be positive")
	}
	m := &maxParallelismResourcePool{}
	m.c = make(chan struct{}, max)
	for i := 0; i < max; i++ {
		m.c <- struct{}{}
	}
	return m, nil
}

func (m *maxParallelismResourcePool) acquire(ctx context.Context) (maxParallelismResource, error) {
	r := m.newMaxParallelismResource()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.c:
	}
	return r, nil
}

func (m *maxParallelismResourcePool) newMaxParallelismResource() maxParallelismResource {
	released := false
	r := func() {
		if released {
			return
		}
		released = true
		m.c <- struct{}{}
	}
	return r
}

type resource interface {
	release()
}
