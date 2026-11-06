//go:build !windows

package toolchain

func executableName(name string) string {
	return name
}

func executableCandidates(name string) []string {
	return []string{name}
}

func platformRoots() []string {
	return []string{"/usr/lib/jvm", "/usr/java", "/opt/java", "/opt/jdk"}
}

func platformPath() string {
	return "/usr/bin:/bin"
}

func addPlatformEnv(add func(string, string), _ string) {
	add("LANG", "C")
	add("LC_ALL", "C")
	add("TZ", "UTC")
}
