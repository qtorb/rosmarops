//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "conpty runs on Windows; on Linux and macOS use tests/journeys/drive/drive.py")
	os.Exit(2)
}
