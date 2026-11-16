// Package hostenv tells whether this review runs inside a container, WSL, a
// virtual machine or Windows Sandbox (BV-249 §5, CTO-58 §3). It only reads
// files (on macOS one sysctl value, on Windows two registry values, both with
// the standard library): it never runs a program and opens no socket. The
// result is an informative notice with no weight in any score. No signal is
// no notice: an absence proves nothing.
package hostenv

import (
	"bufio"
	"bytes"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// The kinds of environment, as report.v2 names them.
const (
	KindContainer      = "container"
	KindWSL            = "wsl"
	KindVirtualMachine = "virtual_machine"
	KindWindowsSandbox = "windows_sandbox"
)

// The signals, as report.v2 names them.
const (
	SignalDockerenv    = "dockerenv"         // /.dockerenv exists
	SignalContainerenv = "containerenv"      // /run/.containerenv exists
	SignalOverlayRoot  = "mountinfo_overlay" // the root is an overlay mount
	SignalProcVersion  = "proc_version"      // /proc/version names Microsoft or WSL
	SignalWSLInterop   = "wsl_interop"       // /proc/sys/fs/binfmt_misc/WSLInterop exists
	SignalDMI          = "dmi"               // /sys/class/dmi/id names a hypervisor
	SignalHVVMMPresent = "hv_vmm_present"    // macOS kern.hv_vmm_present = 1
	SignalBIOSRegistry = "bios_registry"     // Windows BIOS registry names a hypervisor
	SignalWDAGAccount  = "wdag_account"      // C:\Users\WDAGUtilityAccount exists
)

// Container engines and virtual machine types; Unidentified when the signal
// does not say which.
const (
	EngineDocker = "docker"
	EnginePodman = "podman"
	Unidentified = "unidentified"
)

// Env is the environment this review runs in.
type Env struct {
	Kind   string `json:"kind"`
	Signal string `json:"signal"`
	// Engine is the container's engine (container only).
	Engine string `json:"engine,omitempty"`
	// VMType is the virtual machine's type (virtual_machine only).
	VMType string `json:"vm_type,omitempty"`
	// HostMounts are the folders of the real computer mounted in a container,
	// by their mount point inside it (Linux, from /proc/self/mountinfo).
	HostMounts []string `json:"host_mounts,omitempty"`
	// DockerSocket: the computer's Docker socket is in the container.
	DockerSocket bool `json:"docker_socket,omitempty"`
	// Drives are the Windows drives seen from WSL ("C:", …).
	Drives []string `json:"drives,omitempty"`
}

// TestEnvironment reports the kinds whose sheet carries the «Review of a
// test environment» tag (BV-249 §5, Albert 16-11 15:27): not WSL.
func (e *Env) TestEnvironment() bool {
	return e != nil && e.Kind != KindWSL
}

// maxRead caps what a signal file may hold (invariant 6 asks 2 MB for content
// scans; these files are tiny).
const maxRead = 1 << 20

func readSmall(fsys fs.FS, name string) ([]byte, bool) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxRead))
	return b, err == nil
}

func exists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, name)
	return err == nil
}

// DetectLinux reads the Linux signals from root (the filesystem at "/"), in
// order: Docker's and Podman's files, then WSL, then an overlay root, then
// DMI. nil when none applies.
func DetectLinux(root fs.FS) *Env {
	mounts := parseMountinfo(root)
	container := func(engine, signal string) *Env {
		e := &Env{Kind: KindContainer, Signal: signal, Engine: engine}
		e.HostMounts, e.DockerSocket = hostMounts(mounts)
		return e
	}
	switch {
	case exists(root, ".dockerenv"):
		return container(EngineDocker, SignalDockerenv)
	case exists(root, "run/.containerenv"):
		return container(EnginePodman, SignalContainerenv)
	}
	if v, ok := readSmall(root, "proc/version"); ok && wslVersion.Match(v) {
		return &Env{Kind: KindWSL, Signal: SignalProcVersion, Drives: wslDrives(mounts)}
	}
	if exists(root, "proc/sys/fs/binfmt_misc/WSLInterop") {
		return &Env{Kind: KindWSL, Signal: SignalWSLInterop, Drives: wslDrives(mounts)}
	}
	for _, m := range mounts {
		if m.point == "/" && m.fstype == "overlay" {
			return container(Unidentified, SignalOverlayRoot)
		}
	}
	vendor, _ := readSmall(root, "sys/class/dmi/id/sys_vendor")
	product, _ := readSmall(root, "sys/class/dmi/id/product_name")
	if t := VMTypeOf(string(vendor), string(product)); t != "" {
		return &Env{Kind: KindVirtualMachine, Signal: SignalDMI, VMType: t}
	}
	return nil
}

var wslVersion = regexp.MustCompile(`(?i)microsoft|wsl`)

// VMTypeOf is the hypervisor a DMI or BIOS manufacturer and product name
// (CTO-58 §3); "" for none known.
func VMTypeOf(vendor, product string) string {
	v, p := strings.ToLower(strings.TrimSpace(vendor)), strings.ToLower(strings.TrimSpace(product))
	both := v + " " + p
	switch {
	case strings.Contains(both, "qemu") || strings.Contains(both, "kvm"):
		return "qemu"
	case strings.Contains(both, "vmware"):
		return "vmware"
	case strings.Contains(both, "innotek") || strings.Contains(both, "virtualbox"):
		return "virtualbox"
	case strings.Contains(v, "microsoft") && strings.Contains(p, "virtual machine"):
		return "hyper_v"
	case strings.Contains(both, "amazon ec2"):
		return "amazon_ec2"
	case strings.Contains(p, "google compute engine"):
		return "google"
	case strings.Contains(both, "parallels"):
		return "parallels"
	case strings.Contains(both, "xen"):
		return "xen"
	}
	return ""
}

// VMTypeName is a virtual machine type's product name, shown as it is; ""
// for unidentified (the catalog's text).
func VMTypeName(t string) string {
	return map[string]string{
		"qemu": "QEMU", "vmware": "VMware", "virtualbox": "VirtualBox", "hyper_v": "Hyper-V",
		"amazon_ec2": "Amazon EC2", "google": "Google Compute Engine", "parallels": "Parallels", "xen": "Xen",
	}[t]
}

// EngineName is a container engine's product name; "" for unidentified.
func EngineName(e string) string {
	return map[string]string{EngineDocker: "Docker", EnginePodman: "Podman"}[e]
}

type mount struct{ point, fstype string }

// parseMountinfo reads /proc/self/mountinfo: each line's mount point (field
// 5, octal escapes undone) and file system type (first field after "-").
func parseMountinfo(root fs.FS) []mount {
	b, ok := readSmall(root, "proc/self/mountinfo")
	if !ok {
		return nil
	}
	var out []mount
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		sep := -1
		for i, x := range f {
			if x == "-" {
				sep = i
				break
			}
		}
		if len(f) < 5 || sep < 0 || sep+1 >= len(f) {
			continue
		}
		out = append(out, mount{point: unescape(f[4]), fstype: f[sep+1]})
	}
	return out
}

var octal = regexp.MustCompile(`\\[0-7]{3}`)

func unescape(s string) string {
	return octal.ReplaceAllStringFunc(s, func(m string) string {
		n := 0
		for _, c := range m[1:] {
			n = n*8 + int(c-'0')
		}
		return string(rune(n))
	})
}

// virtualFS are the file systems that are no folder of the real computer.
var virtualFS = map[string]bool{
	"overlay": true, "proc": true, "sysfs": true, "tmpfs": true, "devpts": true, "mqueue": true,
	"cgroup": true, "cgroup2": true, "devtmpfs": true, "securityfs": true, "debugfs": true,
	"tracefs": true, "pstore": true, "bpf": true, "hugetlbfs": true, "nsfs": true,
	"binfmt_misc": true, "fusectl": true, "configfs": true, "autofs": true, "shm": true, "ramfs": true,
}

// containerFiles are the files every container engine mounts from the host.
var containerFiles = map[string]bool{"/etc/hosts": true, "/etc/hostname": true, "/etc/resolv.conf": true}

// hostMounts are a container's mounts of the real computer, and whether the
// Docker socket is one of them.
func hostMounts(ms []mount) (points []string, dockerSocket bool) {
	seen := map[string]bool{}
	for _, m := range ms {
		p := m.point
		switch {
		case p == "/var/run/docker.sock" || p == "/run/docker.sock":
			dockerSocket = true
			continue
		case p == "/" || virtualFS[m.fstype] || containerFiles[p],
			p == "/proc" || strings.HasPrefix(p, "/proc/"),
			p == "/sys" || strings.HasPrefix(p, "/sys/"),
			p == "/dev" || strings.HasPrefix(p, "/dev/"):
			continue
		}
		if !seen[p] {
			seen[p] = true
			points = append(points, p)
		}
	}
	sort.Strings(points)
	return points, dockerSocket
}

var drive = regexp.MustCompile(`^/mnt/([a-z])$`)

// wslDrives are the Windows drives WSL mounts under /mnt.
func wslDrives(ms []mount) []string {
	var out []string
	for _, m := range ms {
		if d := drive.FindStringSubmatch(m.point); d != nil && (m.fstype == "9p" || m.fstype == "drvfs" || m.fstype == "v9fs") {
			out = append(out, strings.ToUpper(d[1])+":")
		}
	}
	sort.Strings(out)
	return out
}
