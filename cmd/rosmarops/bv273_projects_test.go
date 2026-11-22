package main

import (
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/scan/projects"
)

// BV-273: a repository counts as checked in full only when the project search
// finished its root; a repository the person listed always counts.
func TestSearchCompleteRepos(t *testing.T) {
	done := []string{"/home/ana/src/a", "/home/ana/work/b", "/srv/listed"}
	full := projects.Result{Status: projects.StatusComplete, Roots: []string{"/home/ana/src", "/home/ana/work"}}
	if got := searchCompleteRepos(done, full); !reflect.DeepEqual(got, done) {
		t.Errorf("complete search: %v", got)
	}
	cut := projects.Result{
		Status: projects.StatusTimeLimit, Roots: []string{"/home/ana/src", "/home/ana/work"},
		Items:   []projects.Item{{Path: "/srv/listed", Source: projects.SourceListed}},
		Skipped: []projects.NotSearched{{Reason: projects.ReasonIncomplete, Count: 1, Paths: []string{"/home/ana/work/deep"}}},
	}
	if got, want := searchCompleteRepos(done, cut), []string{"/home/ana/src/a", "/srv/listed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("one root unfinished: %v, want %v", got, want)
	}
	// More unfinished folders than the search recorded: no root is known to
	// be finished.
	cut.Skipped[0].Count = 5
	if got, want := searchCompleteRepos(done, cut), []string{"/srv/listed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unknown roots: %v, want %v", got, want)
	}
}
