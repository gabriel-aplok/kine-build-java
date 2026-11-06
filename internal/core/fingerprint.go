package core

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	stdhash "hash"
	"path/filepath"
	"sort"

	"github.com/gabriel-aplok/kine-build-java/internal/hash"
)

const fingerprintSchema uint32 = 1

// Fingerprint returns a stable SHA-256 over the task inputs, key, and
// dependency fingerprints.
func Fingerprint(root string, task *Task, depFingerprints map[string]string) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is nil")
	}

	inputs := append([]Input(nil), task.Inputs...)
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Path != inputs[j].Path {
			return inputs[i].Path < inputs[j].Path
		}
		return inputs[i].Mode < inputs[j].Mode
	})

	type digestedInput struct {
		path string
		mode PathMode
		sum  string
	}
	digested := make([]digestedInput, 0, len(inputs))
	for _, input := range inputs {
		path := filepath.Join(root, filepath.FromSlash(input.Path))
		var (
			sum string
			err error
		)
		switch input.Mode {
		case ModeFile:
			sum, err = hash.File(path)
		case ModeTree:
			sum, err = hash.Tree(path)
		default:
			return "", fmt.Errorf("input %q: invalid mode %d", input.Path, input.Mode)
		}
		if err != nil {
			return "", fmt.Errorf("input %q: %w", input.Path, err)
		}
		digested = append(digested, digestedInput{path: input.Path, mode: input.Mode, sum: sum})
	}

	deps := append([]string(nil), task.Deps...)
	sort.Strings(deps)

	sum := sha256.New()
	writeUint32(sum, fingerprintSchema)
	writeString(sum, task.Key)

	writeUint32(sum, uint32(len(digested)))
	for _, input := range digested {
		writeString(sum, input.path)
		writeUint32(sum, uint32(input.mode))
		writeString(sum, input.sum)
	}

	writeUint32(sum, uint32(len(deps)))
	for _, dep := range deps {
		depSum, ok := depFingerprints[dep]
		if !ok {
			return "", fmt.Errorf("dependency %q: fingerprint missing", dep)
		}
		writeString(sum, dep)
		writeString(sum, depSum)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

func writeString(sum stdhash.Hash, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	sum.Write(length[:])
	sum.Write([]byte(value))
}

func writeUint32(sum stdhash.Hash, value uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	sum.Write(buf[:])
}
