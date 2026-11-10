package spacenet

import (
	"context"
	"errors"
	"net/http"

	"github.com/qtorb/rosmarops/internal/state"
)

// Sending and disconnecting, program side (BV-137 points 6 and 8, contract
// qtorb/rosmarops-app docs/api-v1.md §4 and §5). Both run only under a
// Permission, so they are impossible without the person's recorded approval.

// Send POSTs one locked results package to /api/v1/devices/{host_id}/packages
// with the device credential as the Bearer token (base64url of the 32 bytes).
// It returns the HTTP status (0 on a network error) and, on a network error,
// that error. The caller decides what each status means (see the send
// orchestration): 201 sent, 401 forget permission and credential, 429/5xx keep
// for later, the rest terminal.
func Send(ctx context.Context, perm Permission, hostID string, credential, body []byte) (int, error) {
	c, err := perm.httpClient()
	if err != nil {
		return 0, err
	}
	resp, err := do(ctx, c, http.MethodPost, perm.url("/api/v1/devices/"+hostID+"/packages"), b64.EncodeToString(credential), body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// ProblemsConsent is proof that the person approved sending the problem list
// (BV-210, CTO-32 C1). Like the results Permission, it can be obtained only
// inside this package: from ConsentFromState, which reads the checkbox from the
// state itself, or from ConsentFromSendButton, the live «Send» click. Its zero
// value is unusable, so a caller cannot forge consent, and SendProblems refuses
// without it — the same discipline as Send's Permission.
type ProblemsConsent struct{ ok bool }

// ErrNoProblemsConsent is returned when a problem send is attempted without the
// person's permission (the «Send» button or the saved checkbox).
var ErrNoProblemsConsent = errors.New("spacenet: no permission to send problems (press «Send» or tick «Send the next ones without asking»)")

// ConsentFromState returns a usable ProblemsConsent only when the state at
// statePath records the checkbox «Send the next ones without asking»
// (SendProblemsWithoutAsking == true). It reads the state itself (CTO-32 C1, as
// FromState does for results) so a caller cannot hand in a state it built by
// hand. Otherwise it returns ErrNoProblemsConsent and no network can happen.
func ConsentFromState(statePath string) (ProblemsConsent, error) {
	st, err := state.LoadState(statePath)
	if err != nil {
		return ProblemsConsent{}, err
	}
	if !st.SendProblemsWithoutAsking {
		return ProblemsConsent{}, ErrNoProblemsConsent
	}
	return ProblemsConsent{ok: true}, nil
}

// ConsentFromSendButton is the live consent of the «Send» button pressed now.
// A trust test proves it is called from exactly one place: the ans.send branch
// of offerProblems (cmd/rosmarops).
func ConsentFromSendButton() ProblemsConsent { return ProblemsConsent{ok: true} }

// problemsClient returns the client and base URL the problem send uses. In
// production it is the one host-pinned client and the fixed host; tests replace
// it to reach a local server, without weakening the production host-pin.
var problemsClient = func() (*http.Client, string) { return Client(), prodBaseURL }

// SendProblems POSTs a problems.v1 body to /api/v1/problems over the one
// host-pinned client (BV-210 §4), only with the person's consent (CTO-32 C1).
// Unlike Send, it needs no account, space, credential or cookie — the body is
// anonymous and carries no device identifier. The consent can only come from
// ConsentFromState or ConsentFromSendButton; without it the function refuses and
// opens no connection. It returns the HTTP status (0 on a network error) and, on
// a network error, that error (204 received, 400/413 terminal, 429 retry later).
func SendProblems(ctx context.Context, consent ProblemsConsent, body []byte) (int, error) {
	if !consent.ok {
		return 0, ErrNoProblemsConsent
	}
	c, base := problemsClient()
	resp, err := do(ctx, c, http.MethodPost, base+"/api/v1/problems", "", body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// Disconnect revokes this computer's credential at the area with
// DELETE /api/v1/devices/self (Bearer device_credential). It returns the HTTP
// status (0 on a network error). 204 is success; 401 means the credential was
// already revoked, which the caller treats as success too. The local credential
// and permission are cleared by the caller regardless of the network outcome.
func Disconnect(ctx context.Context, perm Permission, credential []byte) (int, error) {
	c, err := perm.httpClient()
	if err != nil {
		return 0, err
	}
	resp, err := do(ctx, c, http.MethodDelete, perm.url("/api/v1/devices/self"), b64.EncodeToString(credential), nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
