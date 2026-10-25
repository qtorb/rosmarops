package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/state"
)

// acceptedCopyName is the copy of the accepted configuration, in the
// reports directory (decision 2026-10-24, memoria, adjustment A, with the
// owner's answer «Copia en reports»): only to list what changed. The
// question is asked on the configuration's hash, never on the copy; the
// copy is used only when its SHA-256 equals state.integrity.config_sha256.
const acceptedCopyName = "config.accepted.json"

// writeAcceptedCopy writes the accepted configuration's bytes next to the
// reports: done together with config_sha256, on acceptance and by
// rosmarops baseline.
func writeAcceptedCopy(reportDir string, raw []byte) error {
	if err := os.MkdirAll(reportDir, 0o700); err != nil {
		return fmt.Errorf("create report dir %s: %w", reportDir, err)
	}
	if err := os.WriteFile(filepath.Join(reportDir, acceptedCopyName), raw, 0o600); err != nil {
		return fmt.Errorf("write accepted configuration copy: %w", err)
	}
	return nil
}

// configChange is one line of the confirmation: a code of
// console.config_change.* and its params.
type configChange struct {
	code   string
	params map[string]string
}

// configChanges lists what changed from the accepted copy to the current
// configuration: each credential marked as rotated, each test-data entry,
// and one line per other top-level key (allow_list per list) that differs.
// Without a trustworthy copy (missing, or its hash is not the accepted
// one), one line: something in the configuration file changed.
func configChanges(prev []byte, prevOK bool, cur []byte, file string) []configChange {
	other := []configChange{{"console.config_change.other", map[string]string{"key": file}}}
	if !prevOK {
		return other
	}
	var a, b map[string]any
	if json.Unmarshal(prev, &a) != nil || json.Unmarshal(cur, &b) != nil {
		return other
	}
	var out []configChange
	flat := func(m map[string]any) map[string]any {
		f := map[string]any{}
		for k, v := range m {
			if k != "allow_list" {
				f[k] = v
				continue
			}
			if al, ok := v.(map[string]any); ok {
				for lk, lv := range al {
					f["allow_list."+lk] = lv
				}
			}
		}
		return f
	}
	fa, fb := flat(a), flat(b)
	keys := map[string]bool{}
	for k := range fa {
		keys[k] = true
	}
	for k := range fb {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		if reflect.DeepEqual(fa[k], fb[k]) {
			continue
		}
		switch k {
		case "allow_list.rotated_credentials":
			was := entrySet(fa[k], func(e map[string]any) string { return str(e["id"]) })
			for _, e := range entries(fb[k]) {
				if id := str(e["id"]); !was[id] {
					out = append(out, configChange{"console.config_change.rotated", map[string]string{"cred_id": id}})
				}
			}
		case "allow_list.test_fixtures":
			key := func(e map[string]any) string { return str(e["repo"]) + "\x00" + str(e["path"]) }
			was := entrySet(fa[k], key)
			for _, e := range entries(fb[k]) {
				if !was[key(e)] {
					out = append(out, configChange{"console.config_change.fixture", map[string]string{"repo": str(e["repo"]), "path": str(e["path"])}})
				}
			}
		default:
			out = append(out, configChange{"console.config_change.other", map[string]string{"key": k}})
		}
	}
	if len(out) == 0 { // an entry removed, or a change of form only
		return other
	}
	return out
}

func entries(v any) []map[string]any {
	list, _ := v.([]any)
	var out []map[string]any
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func entrySet(v any, key func(map[string]any) string) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries(v) {
		out[key(e)] = true
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// canAsk reports whether the person can be asked: the input and the output
// are both a console (BV-52). With either redirected (a scheduled run, an
// output saved to a file), the question is not asked: the change is
// treated as rejected, console.config_unconfirmed.
func canAsk(in, out *os.File) bool { return isTerminal(in) && isTerminal(out) }

// confirmation is what confirmConfig needs: the console, and whether its
// input is one.
type confirmation struct {
	in       io.Reader
	out      io.Writer
	terminal bool
}

// confirmConfig asks whether the person made the configuration change
// (decision 2026-10-24, memoria, adjustment A; informe HTML v3 §9.6). The
// question is asked only when the configuration's hash differs from the
// accepted one. Accepted (y or s, any case): the configuration is the new
// baseline, config_sha256 and the copy together, the binary's hash
// untouched. Any other answer, or no console: false, and the caller applies
// invariant 8 unchanged.
func confirmConfig(cn confirmation, cat catalog.Catalog, st *state.State, stPath, cfgPath, reportDir string) (bool, error) {
	cur, err := os.ReadFile(cfgPath)
	if err != nil {
		return false, fmt.Errorf("read config: %w", err)
	}
	prev, err := os.ReadFile(filepath.Join(reportDir, acceptedCopyName))
	prevOK := err == nil && hash.Bytes(prev) == st.Integrity.ConfigSHA256
	t := func(key string, p map[string]string) string { return cat.Text(key, p) }
	fmt.Fprintln(cn.out, t("console.config_changed", nil))
	for _, c := range configChanges(prev, prevOK, cur, filepath.Base(cfgPath)) {
		fmt.Fprintln(cn.out, "  - "+t(c.code, c.params))
	}
	if !cn.terminal {
		fmt.Fprintln(cn.out, t("console.config_unconfirmed", nil))
		return false, nil
	}
	fmt.Fprint(cn.out, t("console.config_confirm", nil))
	answer, _ := bufio.NewReader(cn.in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "s":
	default:
		fmt.Fprintln(cn.out, t("console.config_rejected", nil))
		return false, nil
	}
	if _, err := config.LoadConfig(cfgPath); err != nil {
		return false, err // an invalid configuration is never accepted
	}
	st.Integrity.ConfigSHA256 = hash.Bytes(cur)
	if err := writeAcceptedCopy(reportDir, cur); err != nil {
		return false, err
	}
	if err := state.SaveState(stPath, *st); err != nil {
		return false, fmt.Errorf("save state: %w", err)
	}
	fmt.Fprintln(cn.out, t("console.config_accepted", nil))
	return true, nil
}
