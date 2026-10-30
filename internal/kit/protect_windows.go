//go:build windows

package kit

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/qtorb/rosmarops/internal/winpath"
)

// On Windows the kit key file is protected with DPAPI for the current user
// (CTO-10 point 1): CryptProtectData and CryptUnprotectData, without UI, with
// a fixed extra entropy. crypt32.dll is loaded by its absolute path in the
// system directory, never by a bare name, which Windows would look up first
// in the program's folder.

const cryptprotectUIForbidden = 0x1

var entropy = []byte("rosmarops-kit-v1")

// dataBlob is DATA_BLOB.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func blobOf(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func protect(plain []byte) ([]byte, error) { return dpapi("CryptProtectData", plain) }

func unprotect(data []byte) ([]byte, error) { return dpapi("CryptUnprotectData", data) }

// dpapi calls CryptProtectData or CryptUnprotectData, which share the
// argument layout: in, description (out for Unprotect, unused), entropy,
// reserved, prompt, flags, out. The output is copied and freed with
// LocalFree.
func dpapi(name string, in []byte) ([]byte, error) {
	sys := winpath.SystemDir()
	if sys == "" {
		return nil, errors.New("no system directory")
	}
	dll, err := syscall.LoadDLL(sys + `\crypt32.dll`)
	if err != nil {
		return nil, fmt.Errorf("load crypt32.dll: %w", err)
	}
	defer dll.Release()
	proc, err := dll.FindProc(name)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", name, err)
	}
	inBlob, entBlob := blobOf(in), blobOf(entropy)
	var out dataBlob
	r, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&inBlob)), 0, uintptr(unsafe.Pointer(&entBlob)),
		0, 0, cryptprotectUIForbidden, uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("%s: %w", name, callErr)
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(unsafe.Pointer(out.pbData)))) // freeing: nothing to do on failure
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}
