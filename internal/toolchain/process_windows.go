//go:build windows

package toolchain

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func prepareExecutable(path string, args []string, dir string) execTarget {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bat", ".cmd":
		return execTarget{
			path: commandInterpreter(),
			args: append([]string{"/d", "/c", filepath.Base(path)}, args...),
			dir:  filepath.Dir(path),
		}
	}
	return execTarget{path: path, args: args, dir: dir}
}

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		HideWindow:    true,
	}
}

func stopProcess(process *os.Process) error {
	command := exec.Command(taskkillPath(), "/T", "/F", "/PID", strconv.Itoa(process.Pid))
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Run(); err != nil {
		if killErr := process.Kill(); killErr != nil {
			return nil
		}
	}
	return nil
}

func commandInterpreter() string {
	if value := os.Getenv("ComSpec"); value != "" {
		return value
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "cmd.exe")
	}
	return "cmd.exe"
}

func taskkillPath() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "taskkill.exe")
	}
	return "taskkill.exe"
}
