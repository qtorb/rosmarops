package spacenet

import (
	"errors"
	"net/http"

	"github.com/qtorb/rosmarops/internal/state"
)

// Permission is proof that the person approved connecting this computer to
// their space (CTO-14 P3). It can be obtained only from a state whose
// SpacePermission is true; its zero value is unusable. It carries the one
// hardened client spacenet talks through — built inside this package by
// Client() — and the base URL requests are built against. Production's base is
// the compile-time constant prodBaseURL; there is no package-level destination
// variable that a linker flag could repoint (CTO-15 PR3). Tests in this package
// build a Permission directly through these unexported fields (the non-exported
// hook), pointing the client and the base at a local server.
type Permission struct {
	client *http.Client
	base   string
}

// url is the absolute URL of path (which starts with "/") for this permission.
func (p Permission) url(path string) string { return p.base + path }

// ErrNoPermission is returned when a space operation is attempted without the
// person's recorded approval, or with the unusable zero Permission.
var ErrNoPermission = errors.New("spacenet: this computer is not connected to a space (no permission)")

// FromState loads the state at statePath and returns a usable Permission only
// when it records the person's approval (SpacePermission == true). Otherwise it
// returns ErrNoPermission and no network can happen. FromState reads the state
// itself (CTO-15 PR3) so a caller cannot hand in a state it built by hand; this
// is the one way production code obtains a Permission, so the permission is
// enforced here, in the only network package. The base is the fixed host
// constant.
func FromState(statePath string) (Permission, error) {
	st, err := state.LoadState(statePath)
	if err != nil {
		return Permission{}, err
	}
	if !st.SpacePermission {
		return Permission{}, ErrNoPermission
	}
	return Permission{client: Client(), base: prodBaseURL}, nil
}

// httpClient returns the permission's client, or ErrNoPermission for the zero
// value. Every network entry point in spacenet goes through it first.
func (p Permission) httpClient() (*http.Client, error) {
	if p.client == nil {
		return nil, ErrNoPermission
	}
	return p.client, nil
}
