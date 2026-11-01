package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/qtorb/rosmarops/internal/spacenet"
)

// The connect window (BV-137 point 5, WinForms pattern of CTO-12 H2). On a
// Windows double click, when this computer is not connected, it asks whether to
// connect; on «Connect» it records the permission and runs the protocol, then
// shows the number to pick and, at the end, the result. It carries no secret.
// macOS has no window: `rosmarops connect` does the same in the console.
//
// This file holds the Go driver that talks to the window over its standard
// streams; it is tested with pipes. The window itself (the PowerShell WinForms
// script and the exec that starts it) is in connectwindow_windows.go.

// driveConnectWindow drives the window over its streams. It reads the first
// line the window writes: "connect" starts the protocol, anything else (or a
// closed stream) means the person declined. On "connect" it runs run, wiring
// onNumber and onOpen to the window, then tells the window the final state and
// waits for its "ok". It returns whether this computer ended up connected.
func driveConnectWindow(out io.Reader, in io.Writer, run func(onNumber func(int), onOpen func(string) error) error) bool {
	r := bufio.NewReader(out)
	first, _ := r.ReadString('\n')
	if strings.TrimRight(first, "\r\n") != "connect" {
		return false
	}
	onNumber := func(n int) { fmt.Fprintf(in, "number %d\n", n) }
	onOpen := func(u string) error { openSpaceURL(u); return nil }
	state := "done"
	switch err := run(onNumber, onOpen); {
	case err == nil:
	case errors.Is(err, spacenet.ErrConnectExpired),
		errors.Is(err, spacenet.ErrConnectCancelled),
		errors.Is(err, context.DeadlineExceeded):
		state = "expired"
	default:
		state = "failed"
	}
	fmt.Fprintf(in, "%s\n", state)
	_, _ = r.ReadString('\n') // the window's "ok", or its stream ending
	return state == "done"
}
