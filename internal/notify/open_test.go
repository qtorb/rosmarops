package notify

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// BV-102, point 1: the report opens with /usr/bin/open and one argument,
// its absolute path; open's failure is the outcome; a relative path is
// refused without running anything.
func TestOpenWith(t *testing.T) {
	var got []string
	ok := func(_ context.Context, name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	}
	if err := openWith(ok, "/Users/me/.rosmarops/reports/report.html"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/usr/bin/open", "/Users/me/.rosmarops/reports/report.html"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}

	boom := errors.New("exit status 1")
	failed := func(context.Context, string, ...string) error { return boom }
	if err := openWith(failed, "/Users/me/report.html"); !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}

	got = nil
	if err := openWith(ok, "-a Terminal"); err == nil || got != nil {
		t.Errorf("relative path: err = %v, ran %q", err, got)
	}
}
