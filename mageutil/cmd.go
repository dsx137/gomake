package mageutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/openimsdk/gomake/internal/priority"
)

type Cmd struct {
	name string
	args []string

	env map[string]string
	dir string

	priority *priority.Level

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func NewCmd(name string) *Cmd {
	return &Cmd{name: name}
}

func (c *Cmd) WithArgs(args ...string) *Cmd {
	c.args = append([]string(nil), args...)
	return c
}

func (c *Cmd) WithEnv(env map[string]string) *Cmd {
	if len(env) == 0 {
		return c
	}
	if c.env == nil {
		c.env = make(map[string]string, len(env))
	}
	for k, v := range env {
		c.env[k] = v
	}
	return c
}

func (c *Cmd) WithDir(dir string) *Cmd {
	c.dir = strings.TrimSpace(dir)
	return c
}

func (c *Cmd) WithPriority(priority priority.Level) *Cmd {
	c.priority = &priority
	return c
}

func (c *Cmd) WithStdin(stdin io.Reader) *Cmd {
	c.stdin = stdin
	return c
}

func (c *Cmd) WithStdout(stdout io.Writer) *Cmd {
	c.stdout = stdout
	return c
}

func (c *Cmd) WithStderr(stderr io.Writer) *Cmd {
	c.stderr = stderr
	return c
}

func (c *Cmd) WithStdio(stdin io.Reader, stdout, stderr io.Writer) *Cmd {
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr
	return c
}

func (c *Cmd) Run() error {
	if strings.TrimSpace(c.name) == "" {
		return errors.New("command is empty")
	}

	execCmd := exec.Command(c.name, c.args...)
	execCmd.Env = append(os.Environ(), flattenEnv(c.env)...)
	if c.dir != "" {
		execCmd.Dir = c.dir
	}

	stdin, stdout, stderr := c.resolveIO()
	execCmd.Stdin = stdin
	execCmd.Stdout = stdout
	execCmd.Stderr = stderr

	if err := execCmd.Start(); err != nil {
		return err
	}

	c.applyPriority(execCmd.Process.Pid)
	return execCmd.Wait()
}

func (c *Cmd) resolveIO() (io.Reader, io.Writer, io.Writer) {
	stdin := c.stdin

	stdout := c.stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := c.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	return stdin, stdout, stderr
}

func (c *Cmd) applyPriority(pid int) {
	if c.priority == nil {
		return
	}
	if err := priority.Set(pid, *c.priority); err != nil {
		PrintYellow(fmt.Sprintf("Failed to set priority for PID %d: %v", pid, err))
	}
}

func flattenEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	flattened := make([]string, 0, len(env))
	for k, v := range env {
		flattened = append(flattened, k+"="+v)
	}
	return flattened
}
