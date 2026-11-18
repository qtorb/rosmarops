//go:build windows

package company

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/qtorb/rosmarops/internal/winpath"
)

// machineDir is <ProgramData>\RosmarOps, with ProgramData resolved through
// SHGetKnownFolderPath(FOLDERID_ProgramData): never %ProgramData%, which the
// person can change.
func machineDir() string {
	pd, err := programData()
	if err != nil || pd == "" {
		return ""
	}
	return filepath.Join(pd, "RosmarOps")
}

// folderIDProgramData is FOLDERID_ProgramData,
// {62AB5D82-FDC1-4DC3-A9DD-070D1D495D97}.
var folderIDProgramData = syscall.GUID{
	Data1: 0x62AB5D82, Data2: 0xFDC1, Data3: 0x4DC3,
	Data4: [8]byte{0xA9, 0xDD, 0x07, 0x0D, 0x1D, 0x49, 0x5D, 0x97},
}

// systemDLL loads a DLL by its absolute path in the system folder, never by
// a bare name that Windows would look up first in the program's folder.
func systemDLL(name string) (*syscall.DLL, error) {
	sys := winpath.SystemDir()
	if sys == "" {
		return nil, errors.New("no system directory")
	}
	return syscall.LoadDLL(sys + `\` + name)
}

func programData() (string, error) {
	shell32, err := systemDLL("shell32.dll")
	if err != nil {
		return "", err
	}
	defer shell32.Release()
	ole32, err := systemDLL("ole32.dll")
	if err != nil {
		return "", err
	}
	defer ole32.Release()
	get, err := shell32.FindProc("SHGetKnownFolderPath")
	if err != nil {
		return "", err
	}
	free, err := ole32.FindProc("CoTaskMemFree")
	if err != nil {
		return "", err
	}
	var p *uint16
	hr, _, _ := get.Call(uintptr(unsafe.Pointer(&folderIDProgramData)), 0, 0, uintptr(unsafe.Pointer(&p)))
	if p != nil {
		defer func() { _, _, _ = free.Call(uintptr(unsafe.Pointer(p))) }()
	}
	if hr != 0 || p == nil {
		return "", fmt.Errorf("SHGetKnownFolderPath: 0x%x", hr)
	}
	// The path ends at its NUL; Windows paths are at most 32,767 characters.
	n := 0
	for s := unsafe.Slice(p, 32768); n < len(s) && s[n] != 0; n++ {
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n)), nil
}

// The identities that may own and write the company folder and its files:
// Administrators, SYSTEM and TrustedInstaller.
var trustedSIDs = map[string]bool{
	"S-1-5-32-544": true,
	"S-1-5-18":     true,
	"S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464": true,
}

// The rights no other identity may hold: to write or append, to change the
// permissions or the owner, or to delete. On a folder FILE_ADD_FILE and
// FILE_ADD_SUBDIRECTORY share the bits of FILE_WRITE_DATA and
// FILE_APPEND_DATA, so they are covered too.
const (
	fileWriteData  = 0x0002
	fileAppendData = 0x0004
	rightDelete    = 0x00010000
	writeDAC       = 0x00040000
	writeOwner     = 0x00080000
	genericAll     = 0x10000000
	genericWrite   = 0x40000000
	forbidden      = fileWriteData | fileAppendData | rightDelete | writeDAC | writeOwner | genericAll | genericWrite
)

const (
	seFileObject              = 1
	ownerSecurityInformation  = 0x1
	daclSecurityInformation   = 0x4
	fileAttributeReparsePoint = 0x400
)

// ACE types: an allow entry is checked; a deny entry takes nothing away
// from the check; any other type makes the object untrusted.
const (
	aceAccessAllowed         = 0x0
	aceAccessDenied          = 0x1
	aceAccessDeniedObject    = 0x6
	aceAccessAllowedCallback = 0x9
	aceAccessDeniedCallback  = 0xA
)

// checkTrusted holds when the file and its folder are not links or other
// reparse points, are owned by Administrators, SYSTEM or TrustedInstaller,
// and no allow entry, CREATOR OWNER and inherit-only entries included, gives
// another identity a forbidden right.
func checkTrusted(path string) error {
	for _, p := range []string{path, filepath.Dir(path)} {
		if err := objectTrusted(p); err != nil {
			return err
		}
	}
	return nil
}

func objectTrusted(p string) error {
	name, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	attrs, err := syscall.GetFileAttributes(name)
	if err != nil {
		return err
	}
	if attrs&fileAttributeReparsePoint != 0 {
		return fmt.Errorf("%w: %s is a link or reparse point", ErrUntrusted, p)
	}
	owner, dacl, free, err := securityInfo(name)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUntrusted, p, err)
	}
	defer free()
	if owner == "" || !trustedSIDs[owner] {
		return fmt.Errorf("%w: %s is owned by %s", ErrUntrusted, p, owner)
	}
	if dacl == nil {
		return fmt.Errorf("%w: %s has no access list (everyone has full control)", ErrUntrusted, p)
	}
	return checkACL(p, dacl)
}

// securityInfo is the owner SID and the DACL of the object, with the function
// that frees them.
func securityInfo(name *uint16) (owner string, dacl []byte, free func(), err error) {
	advapi32, err := systemDLL("advapi32.dll")
	if err != nil {
		return "", nil, nil, err
	}
	get, err := advapi32.FindProc("GetNamedSecurityInfoW")
	if err != nil {
		advapi32.Release()
		return "", nil, nil, err
	}
	var ownerSID *syscall.SID
	var aclPtr *byte
	var sd uintptr
	r, _, _ := get.Call(uintptr(unsafe.Pointer(name)), seFileObject, ownerSecurityInformation|daclSecurityInformation,
		uintptr(unsafe.Pointer(&ownerSID)), 0, uintptr(unsafe.Pointer(&aclPtr)), 0, uintptr(unsafe.Pointer(&sd)))
	free = func() {
		if sd != 0 {
			_, _ = syscall.LocalFree(syscall.Handle(sd))
		}
		advapi32.Release()
	}
	if r != 0 {
		free()
		return "", nil, nil, fmt.Errorf("GetNamedSecurityInfoW: error %d", r)
	}
	if ownerSID != nil {
		if s, sErr := ownerSID.String(); sErr == nil {
			owner = s
		}
	}
	if aclPtr != nil {
		// ACL header: revision, padding, AclSize (uint16), AceCount, padding.
		head := unsafe.Slice(aclPtr, 8)
		size := int(binary.LittleEndian.Uint16(head[2:4]))
		dacl = unsafe.Slice(aclPtr, size)
	}
	return owner, dacl, free, nil
}

// checkACL walks the entries of a DACL in place.
func checkACL(p string, acl []byte) error {
	if len(acl) < 8 {
		return fmt.Errorf("%w: %s: malformed access list", ErrUntrusted, p)
	}
	count := int(binary.LittleEndian.Uint16(acl[4:6]))
	off := 8
	for i := 0; i < count; i++ {
		if off+4 > len(acl) {
			return fmt.Errorf("%w: %s: malformed access list", ErrUntrusted, p)
		}
		typ := acl[off]
		size := int(binary.LittleEndian.Uint16(acl[off+2 : off+4]))
		if size < 4 || off+size > len(acl) {
			return fmt.Errorf("%w: %s: malformed access list", ErrUntrusted, p)
		}
		switch typ {
		case aceAccessDenied, aceAccessDeniedObject, aceAccessDeniedCallback:
		case aceAccessAllowed, aceAccessAllowedCallback:
			if size < 16 {
				return fmt.Errorf("%w: %s: malformed access entry", ErrUntrusted, p)
			}
			mask := binary.LittleEndian.Uint32(acl[off+4 : off+8])
			sid, err := (*syscall.SID)(unsafe.Pointer(&acl[off+8])).String()
			if err != nil {
				return fmt.Errorf("%w: %s: unreadable access entry", ErrUntrusted, p)
			}
			if mask&forbidden != 0 && !trustedSIDs[sid] {
				return fmt.Errorf("%w: %s lets %s change it", ErrUntrusted, p, sid)
			}
		default:
			return fmt.Errorf("%w: %s has an access entry of type %d", ErrUntrusted, p, typ)
		}
		off += size
	}
	return nil
}
