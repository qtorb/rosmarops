package firstrun

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// kitConfigInput is base64 of the config's JSON, the line the window reads
// first: decoding it must give back exactly the config.
func TestKitConfigInput(t *testing.T) {
	cfg := KitConfig{Title: "RosmarOps", Found: "found", Ask: "ask", Date: "d", Use: "use", NotNow: "no", Wrong: "w"}
	raw, err := base64.StdEncoding.DecodeString(kitConfigInput(cfg))
	if err != nil {
		t.Fatalf("not base64: %v", err)
	}
	var got KitConfig
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("not the config JSON: %v", err)
	}
	if got != cfg {
		t.Errorf("round trip changed the config: %+v", got)
	}
}

// fakeWindow drives driveKitWindow from the window's side: it writes a base64
// phrase, then reads the one-byte verdict the driver answers with. It lets a
// test play out «right phrase», «wrong then right» and «gave up / closed»
// without any PowerShell.
type fakeWindow struct {
	phrases []string // what the window «types», in order
	toDrv   *io.PipeWriter
	fromDrv *io.PipeReader
	verdict chan string // each verdict line the driver wrote back
}

func newFakeWindow(phrases []string) (*fakeWindow, io.Reader, io.Writer) {
	// out: window -> driver. in: driver -> window.
	outR, outW := io.Pipe()
	inR, inW := io.Pipe()
	w := &fakeWindow{phrases: phrases, toDrv: outW, fromDrv: inR, verdict: make(chan string, len(phrases)+1)}
	go w.run(inR)
	return w, outR, inW
}

func (w *fakeWindow) run(inR *io.PipeReader) {
	buf := make([]byte, 1024)
	for _, p := range w.phrases {
		enc := base64.StdEncoding.EncodeToString([]byte(p))
		if _, err := io.WriteString(w.toDrv, enc+"\n"); err != nil {
			close(w.verdict)
			return
		}
		n, err := inR.Read(buf)
		if n > 0 {
			w.verdict <- strings.TrimRight(string(buf[:n]), "\r\n")
			if strings.TrimRight(string(buf[:n]), "\r\n") == "1" {
				// opened: the window closes.
				_ = w.toDrv.Close()
				close(w.verdict)
				return
			}
		}
		if err != nil {
			close(w.verdict)
			return
		}
	}
	// ran out of phrases: the window closes with nothing pending.
	_ = w.toDrv.Close()
	close(w.verdict)
}

func TestDriveKitWindow(t *testing.T) {
	cases := []struct {
		name    string
		phrases []string
		opens   string // the phrase check() accepts ("" = none)
		want    bool
	}{
		{"first phrase opens", []string{"open-sesame"}, "open-sesame", true},
		{"wrong then right", []string{"nope", "nope2", "open-sesame"}, "open-sesame", true},
		{"all wrong, window gives up", []string{"a", "b", "c"}, "open-sesame", false},
		{"window closes immediately", []string{}, "open-sesame", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, out, in := newFakeWindow(c.phrases)
			check := func(p string) bool { return c.opens != "" && p == c.opens }
			got := driveKitWindow(out, in, check, MaxKitAttempts)
			if got != c.want {
				t.Errorf("driveKitWindow = %v, want %v", got, c.want)
			}
		})
	}
}

// TestDriveKitWindowStopsAtMaxAttempts proves the driver gives up on its own
// after maxAttempts wrong phrases even if the window would keep asking, so a
// misbehaving window cannot loop forever.
func TestDriveKitWindowStopsAtMaxAttempts(t *testing.T) {
	// A window that always sends the same wrong phrase, forever.
	outR, outW := io.Pipe()
	inR, inW := io.Pipe()
	go func() {
		buf := make([]byte, 16)
		enc := base64.StdEncoding.EncodeToString([]byte("wrong"))
		for {
			if _, err := io.WriteString(outW, enc+"\n"); err != nil {
				return
			}
			if _, err := inR.Read(buf); err != nil {
				return
			}
		}
	}()
	defer outW.Close()
	defer inW.Close()
	calls := 0
	check := func(string) bool { calls++; return false }
	if driveKitWindow(outR, inW, check, 3) {
		t.Fatal("driveKitWindow returned true for an always-wrong window")
	}
	if calls != 3 {
		t.Errorf("check called %d times, want 3 (maxAttempts)", calls)
	}
}

// TestMaxKitAttemptsAgree keeps the Go constant and the number baked into the
// PowerShell script in step.
func TestMaxKitAttemptsAgree(t *testing.T) {
	if maxKitAttemptsStr != "5" || MaxKitAttempts != 5 {
		t.Fatalf("MaxKitAttempts=%d maxKitAttemptsStr=%q, want 5/\"5\"", MaxKitAttempts, maxKitAttemptsStr)
	}
	if !strings.Contains(KitWindowScript, "-ge "+maxKitAttemptsStr) {
		t.Errorf("KitWindowScript does not use maxKitAttemptsStr %q in its give-up test", maxKitAttemptsStr)
	}
}

// TestKitWindowScriptNeverLogsPhrase is a source-level guard for CTO-12 H2: the
// phrase must only ever be the password TextBox value turned into base64 with
// the two .NET calls, never a cmdlet argument. The mandatory runtime logging
// test lives in the Windows-guarded test; this one keeps the mechanism from
// drifting on any platform.
func TestKitWindowScriptNeverLogsPhrase(t *testing.T) {
	if !strings.Contains(KitWindowScript, "UseSystemPasswordChar") {
		t.Error("the phrase TextBox must use UseSystemPasswordChar")
	}
	want := "[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($tb.Text))"
	if !strings.Contains(KitWindowScript, want) {
		t.Errorf("KitWindowScript must emit the phrase via %s", want)
	}
	// The TextBox value never reaches a cmdlet: no Write-* / Out-* of $tb.Text.
	for _, bad := range []string{"Write-Host $tb", "Write-Output $tb", "$tb.Text |", "Out-File"} {
		if strings.Contains(KitWindowScript, bad) {
			t.Errorf("KitWindowScript routes the phrase through %q, which logging can capture", bad)
		}
	}
}
