package main

import (
	"reflect"
	"sort"
	"testing"

	"github.com/qtorb/rosmarops/internal/cli"
)

// BV-49 (G4): the help's catalog codes are the command tree's, so the
// catalog tests that use cli.HelpKeys cover every text the help prints.
func TestHelpKeysMatchTheTree(t *testing.T) {
	seen := map[string]bool{"cli.usage": true, "cli.help.more": true}
	var walk func(c *cli.Command)
	walk = func(c *cli.Command) {
		for _, k := range []string{c.Short, c.Long} {
			if k != "" {
				seen[k] = true
			}
		}
		for _, f := range c.Flags {
			seen[f.Usage] = true
		}
		for _, s := range c.Subs {
			walk(s)
		}
	}
	walk(newRootCmd())
	var got []string
	for k := range seen {
		got = append(got, k)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, cli.HelpKeys) {
		t.Errorf("tree %v\nHelpKeys %v", got, cli.HelpKeys)
	}
}
