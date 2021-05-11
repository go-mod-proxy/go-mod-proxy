package gocmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

func (s *Service) runCmd(ctx context.Context, t *tempGoEnv, args []string) (stdout []byte, strLog string, err error) {
	cmdStdoutStderr := newCmdStdoutStderr()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = t.HomeDir
	cmd.Env = t.Environ.GetSlice()
	cmdStdoutStderr.register(cmd)
	err = cmd.Start()
	if err != nil {
		err = fmt.Errorf("error starting command %s (dir = %#v): %w", formatArgs(args), t.HomeDir, err)
		return
	}
	err = cmd.Wait()
	cmdStdoutStderr.done()
	stdout = cmdStdoutStderr.stdout()
	strLog = cmdStdoutStderr.getLog()
	if err != nil {
		exitCode := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ProcessState.ExitCode()
		}
		err = fmt.Errorf("command %s failed (exitCode = %d, dir = %#v) with stderr/stdout:\n%s", formatArgs(args),
			exitCode, t.HomeDir, cmdStdoutStderr.getLog())
		return
	}
	logger := log.StandardLogger()
	if logLevel := log.TraceLevel; logger.IsLevelEnabled(logLevel) {
		log.NewEntry(logger).Logf(logLevel, "command %s succeeded (dir = %#v) with stderr/stdout:\n%s",
			formatArgs(args), t.HomeDir, cmdStdoutStderr.getLog())
		log.NewEntry(logger).Logf(logLevel, "recursively listing non-directory dir entries within tmpDir %#v", t.TmpDir)
		i := 0
		err = filepath.Walk(t.TmpDir, func(path string, info os.FileInfo, err error) error {
			if info != nil && !info.IsDir() {
				log.NewEntry(logger).Logf(logLevel, "  %#v", path[len(t.TmpDir)+1:])
				i++
			}
			return err
		})
		if err != nil {
			log.Errorf("error recursively listing non-directory dir entries within tmpDir %#v: %v", t.TmpDir, err)
		} else {
			log.NewEntry(logger).Logf(logLevel, "recursively listed %d non-directory dir entr(y)(ies) within tmpDir %#v", i, t.TmpDir)
		}
	}
	return
}

type cmdStdoutStderr struct {
	perFDStates []*perFDState
	log         bytes.Buffer
}

func newCmdStdoutStderr() *cmdStdoutStderr {
	c := &cmdStdoutStderr{
		perFDStates: []*perFDState{
			{},
			{},
		},
	}
	return c
}

func (c *cmdStdoutStderr) done() {
	for i, perFDState := range c.perFDStates {
		if perFDState.lineStartPos < len(perFDState.data) {
			fd := wellKnownFileDescriptor(i)
			line := perFDState.data[perFDState.lineStartPos:]
			fmt.Fprintf(&c.log, "%s: %s\n", fd.String(), string(line))
		}
	}
}

func (c *cmdStdoutStderr) getLog() string {
	return c.log.String()
}

func (c *cmdStdoutStderr) register(cmd *exec.Cmd) {
	cmd.Stdout = newCmdStdoutStderrWriter(c, stdout)
	cmd.Stderr = newCmdStdoutStderrWriter(c, stderr)
}

func (c *cmdStdoutStderr) stdout() []byte {
	return c.perFDStates[int(stdout)].data
}

func (c *cmdStdoutStderr) write(fd wellKnownFileDescriptor, p []byte) {
	perFDState := c.perFDStates[fd]
	perFDState.data = append(perFDState.data, p...)
	for {
		i := bytes.IndexByte(perFDState.data[perFDState.scanStartPos:], '\n')
		if i < 0 {
			perFDState.scanStartPos = len(perFDState.data)
			break
		}
		i += perFDState.scanStartPos
		var line []byte
		if i > perFDState.lineStartPos && perFDState.data[i-1] == '\r' {
			line = perFDState.data[perFDState.lineStartPos : i-1]
		} else {
			line = perFDState.data[perFDState.lineStartPos:i]
		}
		perFDState.lineStartPos = i + 1
		perFDState.scanStartPos = i + 1
		fmt.Fprintf(&c.log, "%s: %s\n", fd.String(), string(line))
	}
}

type cmdStdoutStderrWriter struct {
	c  *cmdStdoutStderr
	fd wellKnownFileDescriptor
}

func newCmdStdoutStderrWriter(c *cmdStdoutStderr, fd wellKnownFileDescriptor) *cmdStdoutStderrWriter {
	return &cmdStdoutStderrWriter{
		c:  c,
		fd: fd,
	}
}

func (c *cmdStdoutStderrWriter) Write(p []byte) (n int, err error) {
	c.c.write(c.fd, p)
	n = len(p)
	return
}

type perFDState struct {
	data         []byte
	lineStartPos int
	scanStartPos int
}

type wellKnownFileDescriptor int

const (
	stdout wellKnownFileDescriptor = iota
	stderr
)

func (w *wellKnownFileDescriptor) String() string {
	switch *w {
	case stderr:
		return "stderr"
	case stdout:
		return "stdout"
	default:
		panic(fmt.Errorf("invalid *wellKnownFileDescriptor %v", *w))
	}
}
