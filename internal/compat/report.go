// Package compat imports Maven and Gradle projects into kine.
package compat

import "fmt"

// Build is the part of a Maven or Gradle project the importer understands.
type Build struct {
	Source       string
	File         string
	Java         int
	Kotlin       string
	MainClass    string
	Compile      []string
	Runtime      []string
	Test         []string
	Provided     []string
	JavaDirs     []string
	TestJavaDirs []string
	Warnings     []string
}

func (b *Build) warn(format string, args ...any) {
	b.Warnings = append(b.Warnings, fmt.Sprintf(format, args...))
}

func (b *Build) dependencyCount() int {
	return len(b.Compile) + len(b.Runtime) + len(b.Test) + len(b.Provided)
}
