//go:build !windows

package jvm

import "os"

func createResourceLink(_, dest, target string) error {
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(target, dest)
}
