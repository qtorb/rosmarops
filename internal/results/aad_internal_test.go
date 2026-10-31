package results

import "testing"

// TestAAD_CanonicalLiteral pins the additional-authenticated-data bytes
// (CTO-11 B2): a sorted-key canonical JSON object, byte-for-byte what the
// browser's canonicalJSON produces, so a package the program seals opens in
// the browser. If this literal changes, the two sides no longer agree.
func TestAAD_CanonicalLiteral(t *testing.T) {
	h := Header{
		Format:           Format,
		Version:          Version,
		KitID:            "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		KeyID:            "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		HostID:           "0123456789abcdef",
		CreatedAt:        "2026-10-31T09:00:00Z",
		RosmaropsVersion: "0.6.0",
	}
	const want = `{"created_at":"2026-10-31T09:00:00Z","format":"rosmarops-results","host_id":"0123456789abcdef","key_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","kit_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","rosmarops_version":"0.6.0","version":1}`
	got, err := h.aad()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("aad bytes:\n got  %s\n want %s", got, want)
	}
}
