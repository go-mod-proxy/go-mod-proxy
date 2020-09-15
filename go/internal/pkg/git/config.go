package git

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
)

type Config = map[string][]KeyValuePair

type KeyValuePair struct {
	Key   string
	Value string
}

func WriteConfig(w io.Writer, cfg Config) (err error) {
	for sectionName, keyValuePairs := range cfg {
		err = writeConfigSectionName(w, sectionName)
		if err != nil {
			return
		}
		for _, keyValuePair := range keyValuePairs {
			err = writeConfigKeyValuePair(w, keyValuePair.Key, keyValuePair.Value)
			if err != nil {
				return
			}
		}
	}
	return
}

func WriteConfigFile(file string, cfg Config) (err error) {
	var buf bytes.Buffer
	err = WriteConfig(&buf, cfg)
	if err != nil {
		return
	}
	err = ioutil.WriteFile(file, buf.Bytes(), 0600)
	return
}

func writeConfigKeyValuePair(w io.Writer, key, value string) (err error) {
	// See https://github.com/git/git/blob/20de7e7e4f4e9ae52e6cc7cfaa6469f186ddb0fa/config.c
	if strings.IndexByte(key, 0) >= 0 {
		err = fmt.Errorf("key contains illegal zero byte")
		return
	}
	if strings.IndexByte(value, 0) >= 0 {
		err = fmt.Errorf("value contains illegal zero byte")
		return
	}
	var quote string
	if (value != "" && (value[0] == ' ' || value[len(value)-1] == ' ')) || strings.ContainsAny(value, "#:") {
		quote = `"`
	}
	_, err = fmt.Fprintf(w, "\t%s = %s", key, quote)
	if err != nil {
		return
	}
	start := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\n':
			_, err = fmt.Fprintf(w, "%s\\n", value[start:i])
			if err != nil {
				return
			}
			start = i + 1
		case '\t':
			_, err = fmt.Fprintf(w, "%s\\t", value[start:i])
			if err != nil {
				return
			}
			start = i + 1
		case '"', '\\':
			_, err = fmt.Fprintf(w, "%s\\", value[start:i])
			if err != nil {
				return
			}
			start = i
		default:
		}
	}
	_, err = fmt.Fprintf(w, "%s%s\n", value[start:], quote)
	return
}

func writeConfigSectionName(w io.Writer, sectionName string) (err error) {
	// See https://github.com/git/git/blob/20de7e7e4f4e9ae52e6cc7cfaa6469f186ddb0fa/config.c
	if strings.IndexByte(sectionName, 0) >= 0 {
		err = fmt.Errorf("section name contains illegal zero byte")
		return
	}
	dot := strings.IndexByte(sectionName, '.')
	if dot >= 0 {
		_, err = fmt.Fprintf(w, `[%s "`, sectionName[:dot])
		if err != nil {
			return
		}
		start := dot + 1
		for i := start; i < len(sectionName); i++ {
			if sectionName[i] == '"' || sectionName[i] == '\\' {
				_, err = w.Write([]byte(sectionName[start:i]))
				if err != nil {
					return
				}
				_, err = w.Write([]byte{'\\'})
				if err != nil {
					return
				}
				start = i
			}
		}
		_, err = w.Write([]byte(sectionName[start:]))
		if err != nil {
			return
		}
		_, err = fmt.Fprintf(w, "\"]\n")
		return
	}
	_, err = fmt.Fprintf(w, "[%s]\n", sectionName)
	return
}
