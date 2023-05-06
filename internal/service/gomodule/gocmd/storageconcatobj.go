package gocmd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

type readerForCreateConcatObj struct {
	prefix  []byte
	goModFD *os.File
	zipFD   *os.File
	readPos int
}

var _ io.ReadSeeker = (*readerForCreateConcatObj)(nil)

// newReaderForCreateConcatObj calls Stat on goModFD to determine it's size.
// The size of goModFD and zipFD must not change while the *readerForCreateConcatObj is used.
func newReaderForCreateConcatObj(commitTime time.Time, goModFD, zipFD *os.File) (c *readerForCreateConcatObj, err error) {
	if zipFD == nil {
		err = fmt.Errorf("zipFD must not be nil")
		return
	}
	var arr [binary.MaxVarintLen64]byte
	n := binary.PutVarint(arr[:], commitTime.Unix())
	var buf bytes.Buffer
	buf.Write(arr[:n])
	goModStat, err := goModFD.Stat()
	if err != nil {
		return
	}
	goModFileSize := goModStat.Size()
	n = binary.PutUvarint(arr[:], uint64(goModFileSize))
	buf.Write(arr[:n])
	c = &readerForCreateConcatObj{
		prefix:  buf.Bytes(),
		goModFD: goModFD,
		zipFD:   zipFD,
	}
	return
}

func (c *readerForCreateConcatObj) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return
	}
	if c.readPos < len(c.prefix) {
		n = copy(p, c.prefix[c.readPos:])
		c.readPos += n
		if len(p) == n {
			return
		}
	}
	if c.readPos == len(c.prefix) {
		var n2 int
		n2, err = c.goModFD.Read(p[n:])
		n += n2
		if err == nil {
			return
		}
		if err != io.EOF {
			return
		}
		c.readPos++
	}
	n2, err := c.zipFD.Read(p[n:])
	n += n2
	return
}

func (c *readerForCreateConcatObj) Seek(offset int64, whence int) (int64, error) {
	if whence != io.SeekStart {
		return 0, fmt.Errorf("whence %d is invalid or not supported (only io.SeekStart is supported)", whence)
	}
	if offset != 0 {
		return 0, fmt.Errorf("offset %d is not supported (only 0 offset must be supported)", offset)
	}
	c.readPos = 0
	err := fdSeekToStart(c.goModFD)
	if err != nil {
		return 0, err
	}
	err = fdSeekToStart(c.zipFD)
	if err != nil {
		return 0, err
	}
	log.Tracef("readerForCreateConcatObj(%p): Seek 0 successfully", c)
	return 0, nil
}

func parseConcatObjCommon(data io.Reader) (commitTime time.Time, goModPrefix []byte, goModToRead int, zipPrefix []byte, err error) {
	var buffer [binary.MaxVarintLen64 * 2]byte
	n, err := data.Read(buffer[:])
	if err != nil && err != io.EOF {
		return
	}
	bufferReader := bytes.NewReader(buffer[:n])
	commitTimeUnix, err := binary.ReadVarint(bufferReader)
	if err != nil {
		err = fmt.Errorf("data does not start with a valid 64-bit varint")
		return
	}
	commitTime = time.Unix(commitTimeUnix, 0)
	goModLengthUint64, err := binary.ReadUvarint(bufferReader)
	if err != nil {
		err = fmt.Errorf("data unexpectedly does not start with two valid 64-bit varints")
		return
	}
	if goModLengthUint64 > uint64(math.MaxInt) {
		err = fmt.Errorf("data's second 64-bit varint (as uint) is too large")
		return
	}
	goModLength := int(goModLengthUint64)
	if goModLength <= bufferReader.Len() {
		goModPrefix = make([]byte, goModLength)
		_, _ = bufferReader.Read(goModPrefix)
		zipPrefix = make([]byte, -bufferReader.Len())
		_, _ = bufferReader.Read(zipPrefix)
	} else {
		goModPrefix = make([]byte, bufferReader.Len())
		_, _ = bufferReader.Read(goModPrefix)
		goModToRead = goModLength - len(goModPrefix)
	}
	return
}
