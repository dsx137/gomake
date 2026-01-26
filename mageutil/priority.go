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

func RunWithPriority(priority PriorityLevel, env map[string]string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	pid := cmd.Process.Pid
	if err := SetPriority(pid, priority); err != nil {
		PrintYellow(fmt.Sprintf("Failed to set priority for PID %d: %v", pid, err))
	}

	return cmd.Wait()
}
