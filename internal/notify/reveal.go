package notify

import (
	"errors"
	"fmt"
	"strings"
)

// revealExe returns the absolute Explorer path to launch, or an error when it
// is not known (BV-135, CTO-12 H4). RevealInExplorer only ever runs Explorer
// by its absolute system path (winpath.ExplorerExe); it never falls back to a
// bare "explorer.exe", which the environment's %PATH% could redirect. When the
// path cannot be resolved, Explorer is simply not opened.
func revealExe(explorerPath string) (string, error) {
	if explorerPath == "" {
		return "", errors.New("reveal: Explorer path not found")
	}
	return explorerPath, nil
}

// revealPathOK reports whether path may be passed to explorer.exe /select
// (BV-131, CTO-12 H4): it must carry no double quote (the argument is quoted,
// so a quote could break out of it) and must not be a UNC path (\\server\…),
// where /select behaves differently and is not wanted.
func revealPathOK(path string) error {
	if strings.Contains(path, `"`) {
		return fmt.Errorf("reveal: path contains a double quote: %q", path)
	}
	p := strings.ReplaceAll(path, "/", `\`)
	if strings.HasPrefix(p, `\\`) {
		return fmt.Errorf("reveal: UNC path not supported: %q", path)
	}
	return nil
}

// revealCmdLine is the exact command line for explorer.exe /select (BV-131):
// explorer.exe /select,"<path>". It is used verbatim as SysProcAttr.CmdLine,
// so the quoting is ours, not the os/exec escaper's.
func revealCmdLine(exe, path string) string {
	return exe + ` /select,"` + path + `"`
}
