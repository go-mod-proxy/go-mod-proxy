package util

import (
	"fmt"
	"strings"
)

type Environ struct {
	isCaseSensitive bool
	environ         []string
	index           map[string]int
}

func NewEnviron(environ []string, isCaseSensitive bool) *Environ {
	e := &Environ{
		isCaseSensitive: isCaseSensitive,
		index:           make(map[string]int, len(environ)),
	}
	for _, nameValuePair := range environ {
		j := strings.IndexByte(nameValuePair, '=')
		if j >= 0 {
			name2 := nameValuePair[:j]
			if !e.isCaseSensitive {
				name2 = strings.ToLower(name2)
			}
			if _, ok := e.index[name2]; !ok {
				e.index[name2] = len(e.environ)
				e.environ = append(e.environ, nameValuePair)
			}
		}
	}
	return e
}

func (e *Environ) Copy() *Environ {
	if e == nil {
		return nil
	}
	eClone := &Environ{
		environ: append([]string{}, e.environ...),
	}
	eClone.index = make(map[string]int, len(e.environ))
	for name2, i := range e.index {
		eClone.index[name2] = i
	}
	return eClone
}

func (e *Environ) ForEach(callback func(name, value string) bool) {
	for _, i := range e.index {
		nameValuePair := e.environ[i]
		j := strings.IndexByte(nameValuePair, '=')
		name := nameValuePair[:j]
		value := nameValuePair[j+1:]
		if !callback(name, value) {
			break
		}
	}
}

func (e *Environ) Get(name string) string {
	value, _ := e.Lookup(name)
	return value
}

func (e *Environ) GetSlice() []string {
	return e.environ
}

func (e *Environ) Lookup(name string) (value string, ok bool) {
	name2 := name
	if !e.isCaseSensitive {
		name2 = strings.ToLower(name2)
	}
	var i int
	i, ok = e.index[name2]
	if ok {
		nameValuePair := e.environ[i]
		j := strings.IndexByte(nameValuePair, '=')
		value = nameValuePair[j+1:]
	} else if strings.IndexByte(name, '=') >= 0 {
		panic(fmt.Errorf(`name must not contain "="`))
	}
	return
}

func (e *Environ) Set(name, value string) {
	name2 := name
	if !e.isCaseSensitive {
		name2 = strings.ToLower(name2)
	}
	if i, ok := e.index[name2]; !ok {
		if strings.IndexByte(name, '=') >= 0 {
			panic(fmt.Errorf(`name must not contain "="`))
		}
		e.index[name2] = len(e.environ)
		e.environ = append(e.environ, name+"="+value)
	} else {
		e.environ[i] = name + "=" + value
	}
}

func (e *Environ) Unset(name string) {
	name2 := name
	if !e.isCaseSensitive {
		name2 = strings.ToLower(name2)
	}
	i, ok := e.index[name2]
	if !ok {
		return
	}
	e.environ = append(e.environ[:i], e.environ[i+1:]...)

	// Reset index.
	// TODO optimize this
	for name2 := range e.index {
		delete(e.index, name2)
	}
	for i, nameValuePair := range e.environ {
		name2 := nameValuePair[:strings.IndexByte(nameValuePair, '=')]
		if !e.isCaseSensitive {
			name2 = strings.ToLower(name2)
		}
		e.index[name2] = i
	}
}
