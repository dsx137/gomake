package mageutil

import (
	"fmt"
	"os"
	"os/exec"
)

type PriorityLevel int

const (
	PriorityLow PriorityLevel = iota // 对应 Idle
	PriorityBelowNormal
	PriorityNormal
	PriorityHigh // 注意：Unix下通常需要 sudo 才能设为 High
)

func RunWithPriority(priority PriorityLevel, env map[string]string, cmd string, args ...string) error {
	execCmd := exec.Command(cmd, args...)
	execCmd.Env = os.Environ()
	for k, v := range env {
		execCmd.Env = append(execCmd.Env, k+"="+v)
	}
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Start(); err != nil {
		return err
	}

	pid := execCmd.Process.Pid
	if err := SetPriority(pid, priority); err != nil {
		PrintYellow(fmt.Sprintf("Failed to set priority for PID %d: %v", pid, err))
	}

	return execCmd.Wait()
}
