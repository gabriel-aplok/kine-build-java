//go:build !windows

package toolchain

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func prepareExecutable(path string, args []string, dir string) execTarget {
	return execTarget{path: path, args: args, dir: dir}
}

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func stopProcess(process *os.Process) error {
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill process group: %w", err)
	}
	return nil
}
