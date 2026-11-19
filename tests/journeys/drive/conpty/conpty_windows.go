//go:build windows

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procCreatePseudoConsole = kernel32.NewProc("CreatePseudoConsole")
	procClosePseudoConsole  = kernel32.NewProc("ClosePseudoConsole")
	procInitAttributeList   = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateAttribute     = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteAttributeList = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateProcessW      = kernel32.NewProc("CreateProcessW")
	procGetExitCodeProcess  = kernel32.NewProc("GetExitCodeProcess")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
	procTerminateProcess    = kernel32.NewProc("TerminateProcess")
)

const (
	attributePseudoConsole     = 0x00020016 // PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE
	extendedStartupInfoPresent = 0x00080000
	startfUseStdHandles        = 0x00000100 // STARTF_USESTDHANDLES
	waitObject0                = 0
)

// startupInfoEx is STARTUPINFOEXW.
type startupInfoEx struct {
	syscall.StartupInfo
	attributeList *byte
}

// session is a child process in a pseudo console.
type session struct {
	pc         uintptr
	in, out    syscall.Handle
	proc       syscall.Handle
	attributes []byte

	mu      sync.Mutex
	raw     []byte
	last    time.Time
	done    chan struct{} // the output pipe is closed
	exited  bool
	code    uint32
	answers []*answer
}

type answer struct {
	rx   interface{ FindStringIndex(string) []int }
	text string
	from int
}

func start(argv []string, cols, rows int) (*session, error) {
	var inR, inW, outR, outW syscall.Handle
	if err := syscall.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe: %w", err)
	}
	if err := syscall.CreatePipe(&outR, &outW, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe: %w", err)
	}
	s := &session{in: inW, out: outR, last: time.Now(), done: make(chan struct{})}
	size := uintptr(uint16(cols)) | uintptr(uint16(rows))<<16
	if hr, _, _ := procCreatePseudoConsole.Call(size, uintptr(inR), uintptr(outW), 0, uintptr(unsafe.Pointer(&s.pc))); hr != 0 {
		return nil, fmt.Errorf("CreatePseudoConsole: HRESULT %#x", hr)
	}
	// The pseudo console holds its own copies of the child's ends.
	syscall.CloseHandle(inR)
	syscall.CloseHandle(outW)

	var n uintptr
	procInitAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&n)))
	s.attributes = make([]byte, n)
	if ok, _, err := procInitAttributeList.Call(uintptr(unsafe.Pointer(&s.attributes[0])), 1, 0, uintptr(unsafe.Pointer(&n))); ok == 0 {
		return nil, fmt.Errorf("InitializeProcThreadAttributeList: %w", err)
	}
	if ok, _, err := procUpdateAttribute.Call(uintptr(unsafe.Pointer(&s.attributes[0])), 0, attributePseudoConsole,
		s.pc, unsafe.Sizeof(s.pc), 0, 0); ok == 0 {
		return nil, fmt.Errorf("UpdateProcThreadAttribute: %w", err)
	}
	var si startupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.attributeList = &s.attributes[0]
	// With null standard handles flagged as given, the program gets the
	// pseudoconsole's. Without the flag it inherits this driver's own, which
	// are the pipes of whoever runs the driver: its output would bypass the
	// pseudoconsole and it would see no terminal (GetConsoleMode fails on a
	// pipe), so it would never ask.
	si.Flags = startfUseStdHandles
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = syscall.EscapeArg(a)
	}
	cmd := utf16.Encode([]rune(strings.Join(quoted, " ") + "\x00"))
	var pi syscall.ProcessInformation
	if ok, _, err := procCreateProcessW.Call(0, uintptr(unsafe.Pointer(&cmd[0])), 0, 0, 0, extendedStartupInfoPresent,
		0, 0, uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi))); ok == 0 {
		return nil, fmt.Errorf("CreateProcess %s: %w", argv[0], err)
	}
	syscall.CloseHandle(pi.Thread)
	s.proc = pi.Process
	go s.read()
	return s, nil
}

func (s *session) read() {
	defer close(s.done)
	buf := make([]byte, 64*1024)
	for {
		var n uint32
		if err := syscall.ReadFile(s.out, buf, &n, nil); err != nil || n == 0 {
			return
		}
		s.mu.Lock()
		s.raw = append(s.raw, buf[:n]...)
		s.last = time.Now()
		s.mu.Unlock()
		s.answerAsked()
	}
}

func (s *session) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return squeeze(plain(string(s.raw)))
}

func (s *session) write(text string) {
	b := []byte(text)
	var n uint32
	_ = syscall.WriteFile(s.in, b, &n, nil)
}

// answerAsked answers each if-asked question that appeared since it last did.
func (s *session) answerAsked() {
	t := s.text()
	for _, a := range s.answers {
		if a.from <= len(t) {
			if loc := a.rx.FindStringIndex(t[a.from:]); loc != nil {
				a.from += loc[1]
				s.write(a.text)
			}
		}
	}
}

func (s *session) poll() bool {
	if s.exited {
		return true
	}
	if r, _, _ := procWaitForSingleObject.Call(uintptr(s.proc), 0); r == waitObject0 {
		procGetExitCodeProcess.Call(uintptr(s.proc), uintptr(unsafe.Pointer(&s.code)))
		s.exited = true
	}
	return s.exited
}

func (s *session) idle() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.last)
}

// close ends the process if it still runs, then the pseudo console, which
// flushes and closes the output.
func (s *session) close() {
	if !s.poll() {
		procTerminateProcess.Call(uintptr(s.proc), 1)
	}
	time.Sleep(300 * time.Millisecond)
	procClosePseudoConsole.Call(s.pc)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
	}
	procDeleteAttributeList.Call(uintptr(unsafe.Pointer(&s.attributes[0])))
	syscall.CloseHandle(s.proc)
}

func main() {
	stepsPath := flag.String("steps", "", "the steps file")
	catalogDir := flag.String("catalog", "", "internal/catalog/messages")
	lang := flag.String("lang", "en", "the language of the catalog texts")
	idleSecs := flag.Int("idle", 30, "seconds of silence that mean an unplanned question")
	cols := flag.Int("cols", 400, "console width (wide, so lines do not wrap)")
	rows := flag.Int("rows", 50, "console height")
	logPath := flag.String("log", "", "the output as written")
	textPath := flag.String("text", "", "the output as plain text")
	flag.Parse()
	argv := flag.Args()
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if *stepsPath == "" || *catalogDir == "" || len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: conpty -steps FILE -catalog DIR [-lang es] [-text FILE] -- command args...")
		os.Exit(2)
	}
	if err := run(*stepsPath, *catalogDir, *lang, *idleSecs, *cols, *rows, *logPath, *textPath, argv); err != nil {
		fmt.Fprintln(os.Stderr, "drive: FAIL:", err)
		os.Exit(1)
	}
}

func run(stepsPath, catalogDir, lang string, idleSecs, cols, rows int, logPath, textPath string, argv []string) error {
	catalog, err := loadCatalog(catalogDir, lang)
	if err != nil {
		return err
	}
	steps, err := parseSteps(stepsPath)
	if err != nil {
		return err
	}
	s, err := start(argv, cols, rows)
	if err != nil {
		return err
	}
	fail := func(why string) error {
		s.close()
		save(s, logPath, textPath)
		lines := strings.Split(plain(string(s.raw)), "\n")
		if len(lines) > 40 {
			lines = lines[len(lines)-40:]
		}
		return fmt.Errorf("%s\n--- last screen ---\n%s\n---", why, strings.Join(lines, "\n"))
	}
	seen := 0
	for _, st := range steps {
		switch st.kind {
		case "if-asked":
			s.mu.Lock()
			s.answers = append(s.answers, &answer{rx: pattern(st.arg, catalog), text: unescape(st.answer), from: seen})
			s.mu.Unlock()
			s.answerAsked()
		case "send":
			s.write(unescape(st.arg))
		case "expect":
			rx := pattern(st.arg, catalog)
			limit := time.Duration(orDefault(st.timeout, 120)) * time.Second
			deadline := time.Now().Add(limit)
			for {
				t := s.text()
				if seen <= len(t) {
					if loc := rx.FindStringIndex(t[seen:]); loc != nil {
						seen += loc[1]
						break
					}
				}
				if time.Now().After(deadline) {
					return fail(fmt.Sprintf("line %d: no %q within %s", st.line, st.arg, limit))
				}
				if s.poll() {
					time.Sleep(500 * time.Millisecond)
					if t := s.text(); seen <= len(t) && rx.FindStringIndex(t[seen:]) != nil {
						continue
					}
					return fail(fmt.Sprintf("line %d: exited (%d) before %q", st.line, s.code, st.arg))
				}
				time.Sleep(200 * time.Millisecond)
			}
		case "expect-exit":
			want, err := strconv.Atoi(st.arg)
			if err != nil {
				return fmt.Errorf("line %d: %w", st.line, err)
			}
			limit := time.Duration(orDefault(st.timeout, 600)) * time.Second
			deadline := time.Now().Add(limit)
			for !s.poll() {
				if time.Now().After(deadline) {
					return fail(fmt.Sprintf("line %d: still running after %s", st.line, limit))
				}
				if s.idle() > time.Duration(idleSecs)*time.Second {
					return fail(fmt.Sprintf("line %d: waiting for an answer the journey did not expect (no output for %d s)", st.line, idleSecs))
				}
				time.Sleep(200 * time.Millisecond)
			}
			if int(s.code) != want {
				return fail(fmt.Sprintf("line %d: exit code %d, want %d", st.line, s.code, want))
			}
		default:
			return errors.New("unknown step " + st.kind)
		}
	}
	s.close()
	save(s, logPath, textPath)
	fmt.Printf("drive: OK (%d steps, exit %d)\n", len(steps), s.code)
	return nil
}

func save(s *session, logPath, textPath string) {
	if logPath != "" {
		_ = os.WriteFile(logPath, s.raw, 0o644)
	}
	if textPath != "" {
		_ = os.WriteFile(textPath, []byte(plain(string(s.raw))), 0o644)
	}
}

func orDefault(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}
