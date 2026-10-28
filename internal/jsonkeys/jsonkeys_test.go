package jsonkeys

import (
	"encoding/json"
	"reflect"
	"testing"
)

type entry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Raw     json.RawMessage   `json:"raw"`
	Ignored string            `json:"-"`
}

type file struct {
	Servers map[string]entry `json:"mcpServers"`
	List    []struct {
		Type string `json:"type"`
	} `json:"list"`
	Ptr *struct {
		Mode string `json:"defaultMode"`
	} `json:"ptr"`
}

// The scenario of E1: "command" and "Command" in one entry. encoding/json
// would keep the last; Decode keeps the exact key, as the agent does, and
// reports the other. Map keys (env) and unknown keys are never conflicts.
func TestDecodeExactKeys(t *testing.T) {
	raw := []byte(`{
		"mcpServers": {"a/b": {"command": "powershell", "args": ["-c", "x"], "Command": "npx", "ARGS": ["ok"],
			"env": {"Token": "1", "token": "2"}, "raw": {"Command": 1}, "unknown": 1}},
		"MCPServers": {"x": {"command": "evil"}},
		"list": [{"type": "command"}, {"Type": "prompt"}],
		"ptr": {"DefaultMode": "bypassPermissions", "defaultMode": "default"}
	}`)
	var f file
	conflicts, err := Decode(raw, &f)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/MCPServers", "/list/1/Type", "/mcpServers/a~1b/ARGS", "/mcpServers/a~1b/Command", "/ptr/DefaultMode"}
	if !reflect.DeepEqual(conflicts, want) {
		t.Errorf("conflicts %q, want %q", conflicts, want)
	}
	e := f.Servers["a/b"]
	if e.Command != "powershell" || !reflect.DeepEqual(e.Args, []string{"-c", "x"}) || len(f.Servers) != 1 {
		t.Errorf("decoded %+v", f.Servers)
	}
	if e.Env["Token"] != "1" || e.Env["token"] != "2" || string(e.Raw) != `{"Command":1}` {
		t.Errorf("env %v raw %s", e.Env, e.Raw)
	}
	if f.List[1].Type != "" || f.Ptr.Mode != "default" {
		t.Errorf("list %+v ptr %+v", f.List, f.Ptr)
	}
}

// A file with only exact keys decodes as json.Unmarshal does, numbers
// included; errors are json.Unmarshal's.
func TestDecodeSameAsUnmarshal(t *testing.T) {
	type num struct {
		N    int64   `json:"n"`
		F    float64 `json:"f"`
		Name string
	}
	raw := []byte(`{"n": 9007199254740993, "f": 1.5, "Name": "x"}`)
	var a, b num
	if c, err := Decode(raw, &a); err != nil || len(c) != 0 {
		t.Fatal(c, err)
	}
	if err := json.Unmarshal(raw, &b); err != nil || a != b {
		t.Errorf("%+v vs %+v", a, b)
	}
	if _, err := Decode([]byte(`{"n": "x"}`), &a); err == nil {
		t.Error("type error lost")
	}
	if _, err := Decode([]byte(`{`), &a); err == nil {
		t.Error("syntax error lost")
	}
}
