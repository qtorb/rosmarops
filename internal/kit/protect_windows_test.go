//go:build windows

package kit

import (
	"bytes"
	"testing"
)

// DPAPI round trip for the current user: the protected blob is not the
// record, and it opens back to it (CTO-10 point 1).
func TestDPAPIRoundTrip(t *testing.T) {
	plain := []byte(`{"version":1,"key":"not a real key"}`)
	blob, err := protect(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, plain) || bytes.Contains(blob, []byte("not a real key")) {
		t.Fatal("the DPAPI blob holds the record in the clear")
	}
	got, err := unprotect(blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("DPAPI round trip changed the record")
	}
	blob[len(blob)-1] ^= 0xff
	if _, err := unprotect(blob); err == nil {
		t.Error("a damaged blob opened")
	}
}
