//go:build linux

package projects

import "syscall"

// Linux file system magic numbers of network and FUSE file systems
// (statfs(2) f_type): nfs, cifs, smb2/smb3, fuse (sshfs, rclone…) and 9p.
var networkMagic = map[int64]bool{
	0x6969:     true, // NFS_SUPER_MAGIC
	0xFF534D42: true, // CIFS_SUPER_MAGIC
	0xFE534D42: true, // SMB2_SUPER_MAGIC (smb3)
	0x517B:     true, // SMB_SUPER_MAGIC
	0x65735546: true, // FUSE_SUPER_MAGIC
	0x01021997: true, // V9FS_MAGIC (9p)
}

// networkPath reports a path on a network or FUSE file system (statfs).
func networkPath(p string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return false
	}
	return networkMagic[int64(st.Type)]
}
