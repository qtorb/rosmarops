package spacenet

import (
	"context"
	"net/http"
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
	resp, err := do(ctx, c, http.MethodPost, URL("/api/v1/devices/"+hostID+"/packages"), b64.EncodeToString(credential), body)
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
	resp, err := do(ctx, c, http.MethodDelete, URL("/api/v1/devices/self"), b64.EncodeToString(credential), nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
