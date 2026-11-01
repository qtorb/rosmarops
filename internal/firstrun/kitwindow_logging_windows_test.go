//go:build windows

package firstrun

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/winpath"
)

// TestKitPhraseNeverReachesPowerShellLogs is the mandatory H2 proof (BV-131,
// CTO-12 H2). On a throwaway Windows machine (the windows-latest CI runner, not
// Albert's computer) it turns on, by policy, the three ways PowerShell can keep
// a record of what it runs — module logging (event 4103), script-block logging
// (4104) and transcription — then feeds a marked secret phrase through the very
// mechanism the phrase window uses (KitEchoScript: the TextBox value handled
// only with .NET methods, out as base64 via [Console]::Out). It then reads the
// Microsoft-Windows-PowerShell/Operational log and the transcript folder and
// fails if the phrase appears anywhere. If it ever does, STOP: the window has
// to move to the Windows credential dialog (CTO-12 H2, decided apart).
//
// The search is for an ASCII marker embedded in the phrase, never for the whole
// phrase: logs store text as UTF-16, so a marker of plain bytes is found
// whatever the log's encoding, and the marker is unique enough that its absence
// proves the phrase's. The marker is never passed to any cmdlet or script here
// (not even to the log-reading command), so the act of searching cannot itself
// write it to a log and fake a leak.
func TestKitPhraseNeverReachesPowerShellLogs(t *testing.T) {
	ps := winpath.PowerShellExe()
	if ps == "" {
		var err error
		if ps, err = exec.LookPath("powershell.exe"); err != nil {
			t.Skip("powershell.exe not found")
		}
	}

	const polRoot = `HKLM\SOFTWARE\Policies\Microsoft\Windows\PowerShell`
	sb := polRoot + `\ScriptBlockLogging`
	ml := polRoot + `\ModuleLogging`
	tr := polRoot + `\Transcription`

	// Never clobber a machine that already has these policies: a real box might,
	// the CI runner does not. Skip rather than overwrite and then delete them.
	for _, k := range []string{sb, ml, tr} {
		if regKeyExists(k) {
			t.Skipf("PowerShell logging policy already set (%s); not overwriting a real machine", k)
		}
	}

	transcripts := t.TempDir()
	// Turn the three records on by policy. reg.exe needs the admin rights the CI
	// runner has; without them this cannot be proven here, so skip.
	sets := [][]string{
		{"add", sb, "/v", "EnableScriptBlockLogging", "/t", "REG_DWORD", "/d", "1", "/f"},
		{"add", ml, "/v", "EnableModuleLogging", "/t", "REG_DWORD", "/d", "1", "/f"},
		{"add", ml + `\ModuleNames`, "/v", "*", "/t", "REG_SZ", "/d", "*", "/f"},
		{"add", tr, "/v", "EnableTranscripting", "/t", "REG_DWORD", "/d", "1", "/f"},
		{"add", tr, "/v", "EnableInvocationHeader", "/t", "REG_DWORD", "/d", "1", "/f"},
		{"add", tr, "/v", "OutputDirectory", "/t", "REG_SZ", "/d", transcripts, "/f"},
	}
	for _, args := range sets {
		if out, err := exec.Command("reg.exe", args...).CombinedOutput(); err != nil {
			t.Skipf("cannot set PowerShell logging policy (need admin): reg %v: %v: %s", args, err, out)
		}
	}
	t.Cleanup(func() {
		for _, k := range []string{sb, ml, tr} {
			_ = exec.Command("reg.exe", "delete", k, "/f").Run()
		}
	})

	// The marked phrase. The marker is ASCII; «ñ» and a space make it a realistic
	// secret, but the assertion keys off the marker so log encoding cannot hide a
	// leak.
	const marker = "RSMRPSLEAK7f3a9c2b"
	const phrase = "contraseña de prueba " + marker

	start := time.Now().Add(-2 * time.Second)

	// Run the real mechanism: KitEchoScript reads base64 on stdin, handles it with
	// the same .NET calls as the window's «Use it», and writes base64 back.
	echo := exec.Command(ps, KitEchoArgs()...)
	echo.Env = winpath.ChildEnv(os.Environ())
	echo.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(phrase)) + "\n")
	var eout, eerr bytes.Buffer
	echo.Stdout, echo.Stderr = &eout, &eerr
	if err := echo.Run(); err != nil {
		t.Fatalf("KitEchoScript: %v: %s", err, eerr.String())
	}
	back, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(eout.String()))
	if derr != nil || string(back) != phrase {
		t.Fatalf("round trip changed the phrase: %q (err %v)", string(back), derr)
	}

	// Let transcription flush the file the echo process wrote.
	time.Sleep(500 * time.Millisecond)

	// 1) The transcript folder must not hold the marker.
	if hit := walkForMarker(t, transcripts, marker); hit != "" {
		t.Fatalf("STOP (CTO-12 H2): the phrase was found in a PowerShell transcript (%s); move to the Windows credential dialog", hit)
	}

	// 2) The Operational log must not hold the marker. Dump every message since
	// `start` — the dump command carries no marker — and search in Go.
	msgs := operationalMessagesSince(t, ps, start)
	if containsMarker(msgs, marker) {
		t.Fatal("STOP (CTO-12 H2): the phrase was found in Microsoft-Windows-PowerShell/Operational; move to the Windows credential dialog")
	}
}

// containsMarker finds the ASCII marker whether the text is stored as UTF-8 or
// as UTF-16LE (Windows PowerShell transcripts are Unicode), so a real leak
// cannot slip past an ASCII-only search.
func containsMarker(b []byte, marker string) bool {
	if bytes.Contains(b, []byte(marker)) {
		return true
	}
	u16 := make([]byte, 0, len(marker)*2)
	for i := 0; i < len(marker); i++ {
		u16 = append(u16, marker[i], 0)
	}
	return bytes.Contains(b, u16)
}

func regKeyExists(key string) bool {
	return exec.Command("reg.exe", "query", key).Run() == nil
}

func walkForMarker(t *testing.T, dir, marker string) string {
	t.Helper()
	var hit string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		if containsMarker(b, marker) {
			hit = p
		}
		return nil
	})
	return hit
}

// operationalMessagesSince returns every PowerShell Operational-log message
// since start, as bytes. The command it runs names no marker, so reading the
// log cannot itself plant one.
func operationalMessagesSince(t *testing.T, ps string, start time.Time) []byte {
	t.Helper()
	script := "$ErrorActionPreference='SilentlyContinue';" +
		"$s=[datetime]'" + start.Format("2006-01-02T15:04:05") + "';" +
		"Get-WinEvent -FilterHashtable @{LogName='Microsoft-Windows-PowerShell/Operational';StartTime=$s} |" +
		" ForEach-Object { $_.Message }"
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = winpath.ChildEnv(os.Environ())
	out, _ := cmd.CombinedOutput()
	return out
}
