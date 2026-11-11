//go:build !windows

package dm

import (
	"errors"
	"strings"
)

// Without the Windows key store the password is only encoded (the file is readable by its owner alone).
func dlSeal(b []byte) ([]byte, error) { return append([]byte("plain:"), b...), nil }

func dlUnseal(b []byte) ([]byte, error) {
	if !strings.HasPrefix(string(b), "plain:") {
		return nil, errors.New("unknown format")
	}
	return b[len("plain:"):], nil
}
