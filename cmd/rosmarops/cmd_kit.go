package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// The kit commands (BV-118, point 4): create, import and new-phrase, in the
// console. None uses the network. The phrase, the code and the key are never
// logged; the phrase and the code are shown once, in the console, when they
// are made.

// kitInput and kitOutput are the console, as package vars for the tests.
var (
	kitInput  io.Reader = os.Stdin
	kitOutput io.Writer = os.Stdout
)

// kitMaxFileBytes caps a kit file read by import; a real one is about 1 KB.
const kitMaxFileBytes = 64 << 10

func newKitCmd() *cli.Command {
	lang := cli.Flag{Name: "lang", Default: systemLang(), Usage: "cli.flag.lang"}
	return &cli.Command{
		Name: "kit", Short: "cli.cmd.kit.short", Long: "cli.cmd.kit.long",
		Subs: []*cli.Command{
			{Name: "create", Short: "cli.cmd.kit.create.short", Flags: []cli.Flag{lang}, Args: 0, Before: integrityCheck, Run: runKitCreate},
			{Name: "import", Usage: "<file>", Short: "cli.cmd.kit.import.short", Flags: []cli.Flag{lang}, Args: 1, Before: integrityCheck, Run: runKitImport},
			{Name: "new-phrase", Short: "cli.cmd.kit.new_phrase.short", Flags: []cli.Flag{lang}, Args: 0, Before: integrityCheck, Run: runKitNewPhrase},
		},
	}
}

// kitPaths are where the kit commands read and write: the kit key file, the
// state, and the reports directory (the configured one, else the default).
type kitPaths struct {
	store, state, reports string
}

func resolveKitPaths() (kitPaths, error) {
	store, err := kit.StorePath()
	if err != nil {
		return kitPaths{}, err
	}
	st, err := state.StatePath()
	if err != nil {
		return kitPaths{}, err
	}
	reports, err := report.DefaultReportDir()
	if err != nil {
		return kitPaths{}, err
	}
	cfgPath, err := config.ResolveConfigPath()
	if err != nil {
		return kitPaths{}, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	switch {
	case err == nil && cfg.ReportDirectory != "":
		reports = cfg.ReportDirectory
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return kitPaths{}, fmt.Errorf("load config: %w", err)
	}
	return kitPaths{store: store, state: st, reports: reports}, nil
}

// console reads answers line by line and writes catalog texts.
type console struct {
	in  *bufio.Reader
	out io.Writer
	cat catalog.Catalog
}

func newConsole(inv cli.Invocation) (*console, error) {
	cat, err := catalog.Load(inv.Flags["lang"])
	if err != nil {
		return nil, fmt.Errorf("lang_unsupported: %q (en, es)", inv.Flags["lang"])
	}
	return &console{in: bufio.NewReader(kitInput), out: kitOutput, cat: cat}, nil
}

func (c *console) say(key string, params map[string]string) {
	fmt.Fprintln(c.out, c.cat.Text(key, params))
}

// ask writes the text of key and reads one line, without its line ending. An
// input that ends before a line is an error: nobody is there to answer.
func (c *console) ask(key string) (string, error) {
	c.say(key, nil)
	line, err := c.in.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", fmt.Errorf("read answer: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// choosePhrase asks for an own phrase of 16 characters or more, typed twice,
// or makes one of five words when the answer is empty.
func (c *console) choosePhrase() (string, error) {
	for {
		p, err := c.ask("kit.phrase_prompt")
		if err != nil {
			return "", err
		}
		if p == "" {
			gen, err := kit.GeneratePhrase()
			if err != nil {
				return "", err
			}
			c.say("kit.phrase_generated", map[string]string{"phrase": gen})
			return gen, nil
		}
		if !kit.PhraseLongEnough(p) {
			c.say("kit.phrase_too_short", nil)
			continue
		}
		again, err := c.ask("kit.phrase_again")
		if err != nil {
			return "", err
		}
		if again != p {
			c.say("kit.phrase_mismatch", nil)
			continue
		}
		return p, nil
	}
}

// checkCode shows the recovery code and asks for its last 4 characters until
// they match.
func (c *console) checkCode(code string) error {
	c.say("kit.recovery_code", map[string]string{"code": kit.GroupCode(code)})
	for {
		typed, err := c.ask("kit.recovery_check")
		if err != nil {
			return err
		}
		if four, ok := kit.NormalizeChars(typed); ok && four == code[len(code)-4:] {
			return nil
		}
		c.say("kit.recovery_check_failed", nil)
	}
}

func runKitCreate(inv cli.Invocation) error {
	c, err := newConsole(inv)
	if err != nil {
		return err
	}
	paths, err := resolveKitPaths()
	if err != nil {
		return err
	}
	return kitCreate(c, paths)
}

func kitCreate(c *console, paths kitPaths) error {
	if _, err := os.Stat(paths.store); err == nil {
		return fmt.Errorf("this computer already has a kit key: %s", paths.store)
	}
	st, err := state.LoadState(paths.state)
	if err != nil {
		return fmt.Errorf("load state (run rosmarops init first): %w", err)
	}
	salt, err := st.Salt()
	if err != nil {
		return err
	}
	phrase, err := c.choosePhrase()
	if err != nil {
		return err
	}
	code, err := kit.NewRecoveryCode()
	if err != nil {
		return err
	}
	if err := c.checkCode(code); err != nil {
		return err
	}
	sec, err := kit.NewSecret(salt)
	if err != nil {
		return err
	}
	kitID, err := kit.NewKitID()
	if err != nil {
		return err
	}
	f, err := kit.Seal(sec, kitID, phrase, code)
	if err != nil {
		return err
	}
	stored, err := kit.FromFile(f, sec)
	if err != nil {
		return err
	}
	out, err := writeKitFile(paths.reports, f)
	if err != nil {
		return err
	}
	if err := kit.Save(paths.store, stored); err != nil {
		return err
	}
	c.say("kit.saved", map[string]string{"path": out})
	return nil
}

func runKitImport(inv cli.Invocation) error {
	c, err := newConsole(inv)
	if err != nil {
		return err
	}
	paths, err := resolveKitPaths()
	if err != nil {
		return err
	}
	return kitImport(c, paths, inv.Args[0])
}

// kitImport opens a kit file with its phrase or code, keeps its key on this
// computer and makes its user salt the state's local_salt (D-MD1): when it
// differs, the next report says once that it cannot be compared with the
// previous one (memory.ReasonSaltChanged).
func kitImport(c *console, paths kitPaths, file string) error {
	f, err := readKitFile(file)
	if err != nil {
		return err
	}
	st, err := state.LoadState(paths.state)
	if err != nil {
		return fmt.Errorf("load state (run rosmarops init first): %w", err)
	}
	var sec kit.Secret
	for {
		typed, err := c.ask("kit.import_prompt")
		if err != nil {
			return err
		}
		sec, err = kit.Open(f, typed)
		if err == nil {
			break
		}
		if !errors.Is(err, kit.ErrWrongSecret) {
			return err
		}
		c.say("kit.import_wrong", nil)
	}
	stored, err := kit.FromFile(f, sec)
	if err != nil {
		return err
	}
	if err := kit.Save(paths.store, stored); err != nil {
		return err
	}
	if salt := hex.EncodeToString(sec.Salt); st.LocalSalt != salt {
		st.LocalSalt = salt
		if err := state.SaveState(paths.state, st); err != nil {
			return err
		}
	}
	c.say("kit.imported", nil)
	return nil
}

func runKitNewPhrase(inv cli.Invocation) error {
	c, err := newConsole(inv)
	if err != nil {
		return err
	}
	paths, err := resolveKitPaths()
	if err != nil {
		return err
	}
	return kitNewPhrase(c, paths)
}

// kitNewPhrase sets a new phrase without the old one: the stored key is
// wrapped again, the code envelope is copied unchanged, and the key, kit_id
// and key_id stay the same (CTO-10 point 8).
func kitNewPhrase(c *console, paths kitPaths) error {
	stored, err := kit.Load(paths.store)
	if errors.Is(err, fs.ErrNotExist) {
		c.say("kit.no_kit", nil)
		return errors.New("no kit key on this computer")
	}
	if err != nil {
		return err
	}
	c.say("kit.new_phrase_intro", nil)
	phrase, err := c.choosePhrase()
	if err != nil {
		return err
	}
	f, err := stored.NewPhrase(phrase)
	if err != nil {
		return err
	}
	if _, err := writeKitFile(paths.reports, f); err != nil {
		return err
	}
	c.say("kit.new_phrase_done", nil)
	return nil
}

// writeKitFile writes f to the reports directory as rosmarops-kit-<kit_id[:8]>.json,
// its only path (BV-118, point 2d).
func writeKitFile(reports string, f kit.File) (string, error) {
	raw, err := kit.Marshal(f)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(reports, 0o700); err != nil {
		return "", fmt.Errorf("create report dir %s: %w", reports, err)
	}
	path := filepath.Join(reports, kit.ExportName(f.KitID))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", fmt.Errorf("write kit file: %w", err)
	}
	return path, nil
}

// readKitFile reads and validates a kit file to import.
func readKitFile(path string) (kit.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return kit.File{}, fmt.Errorf("read kit file: %w", err)
	}
	if info.Size() > kitMaxFileBytes {
		return kit.File{}, fmt.Errorf("kit file %s is %d bytes, more than %d", path, info.Size(), kitMaxFileBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return kit.File{}, fmt.Errorf("read kit file: %w", err)
	}
	return kit.Parse(raw)
}
