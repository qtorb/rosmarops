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
// Client() — so no caller supplies a client of its own, and nothing in spacenet
// reaches the network without a Permission whose client is set. Tests in this
// package build one directly through the unexported field (the non-exported
// hook the brief asks for), pointing it at a local server.
type Permission struct {
	client *http.Client
}

// ErrNoPermission is returned when a space operation is attempted without the
// person's recorded approval, or with the unusable zero Permission.
var ErrNoPermission = errors.New("spacenet: this computer is not connected to a space (no permission)")

// FromState returns a usable Permission only when the state records the
// person's approval (SpacePermission == true). Otherwise it returns
// ErrNoPermission and no network can happen. This is the one way production
// code obtains a Permission, so the permission is enforced here, in the only
// network package, rather than left to each caller.
func FromState(st state.State) (Permission, error) {
	if !st.SpacePermission {
		return Permission{}, ErrNoPermission
	}
	return Permission{client: Client()}, nil
}

// httpClient returns the permission's client, or ErrNoPermission for the zero
// value. Every network entry point in spacenet goes through it first.
func (p Permission) httpClient() (*http.Client, error) {
	if p.client == nil {
		return nil, ErrNoPermission
	}
	return p.client, nil
}
