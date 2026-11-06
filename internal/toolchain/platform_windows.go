//go:build windows

package toolchain

import (
	"os"
	"path/filepath"
)

func executableName(name string) string {
	return name + ".exe"
}

func executableCandidates(name string) []string {
	return []string{name + ".exe", name}
}

func platformRoots() []string {
	var roots []string
	for _, variable := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		dir := os.Getenv(variable)
		if dir == "" {
			continue
		}
		roots = append(roots,
			filepath.Join(dir, "Java"),
			filepath.Join(dir, "Eclipse Adoptium"),
			filepath.Join(dir, "Microsoft"),
			filepath.Join(dir, "Zulu"),
			filepath.Join(dir, "BellSoft"),
			filepath.Join(dir, "Amazon Corretto"),
			filepath.Join(dir, "Semeru"),
			filepath.Join(dir, "IBM", "Semeru"),
		)
	}
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		roots = append(roots,
			filepath.Join(dir, "Programs", "Eclipse Adoptium"),
			filepath.Join(dir, "Programs", "Microsoft"),
		)
	}
	return roots
}

func platformPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		return ""
	}
	return filepath.Join(root, "System32") + string(os.PathListSeparator) + root
}

func addPlatformEnv(add func(string, string), tmpDir string) {
	add("TEMP", tmpDir)
	add("TMP", tmpDir)
	if home, err := os.UserHomeDir(); err == nil {
		add("USERPROFILE", home)
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		add("SYSTEMROOT", root)
		add("WINDIR", root)
		add("COMSPEC", filepath.Join(root, "System32", "cmd.exe"))
	}
	add("PATHEXT", ".COM;.EXE;.BAT;.CMD")
}
