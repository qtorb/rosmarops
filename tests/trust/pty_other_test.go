//go:build !linux

package trust_test

import (
	"errors"
	"os"
)

// openPTY is only implemented on Linux; the tests that need it skip elsewhere.
func openPTY() (master, slave *os.File, err error) {
	return nil, nil, errors.New("pseudo-terminal not implemented on this system")
}
