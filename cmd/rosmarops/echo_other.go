//go:build !linux && !darwin && !windows

package main

import (
	"errors"
	"os"
)

// echoOff is not available here: the answer is read with echo.
func echoOff(*os.File) (func(), error) { return nil, errors.New("echo control not supported") }
