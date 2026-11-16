package hostenv_test

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/qtorb/rosmarops/internal/hostenv"
)

const dockerMountinfo = `1 0 0:50 / / rw,relatime - overlay overlay rw,lowerdir=/x
2 1 0:52 / /proc rw - proc proc rw
3 1 0:53 / /dev rw - tmpfs tmpfs rw
4 1 254:1 /docker/containers/abc/hostname /etc/hostname rw - ext4 /dev/vda1 rw
5 1 254:1 /home/ana/src /workspace rw - ext4 /dev/vda1 rw
6 1 254:1 /home/ana/My\040Notes /notes rw - ext4 /dev/vda1 rw
7 1 0:24 /docker.sock /run/docker.sock rw - tmpfs tmpfs rw
8 1 0:54 / /sys ro - sysfs sysfs ro
`

const wslMountinfo = `1 0 8:32 / / rw - ext4 /dev/sdc rw
2 1 0:60 / /mnt/c rw - 9p C:\134 rw
3 1 0:61 / /mnt/d rw - 9p D:\134 rw
4 1 0:62 / /mnt/wslg rw - tmpfs none rw
`

func TestDetectLinux(t *testing.T) {
	for _, c := range []struct {
		name string
		fs   fstest.MapFS
		want *hostenv.Env
	}{
		{"nothing", fstest.MapFS{"proc/version": {Data: []byte("Linux version 6.1 (gcc)")}}, nil},
		{"docker", fstest.MapFS{".dockerenv": {}, "proc/self/mountinfo": {Data: []byte(dockerMountinfo)}}, &hostenv.Env{
			Kind: hostenv.KindContainer, Signal: hostenv.SignalDockerenv, Engine: hostenv.EngineDocker,
			HostMounts: []string{"/notes", "/workspace"}, DockerSocket: true,
		}},
		{"podman", fstest.MapFS{"run/.containerenv": {Data: []byte(`engine="podman-4.9"`)}}, &hostenv.Env{
			Kind: hostenv.KindContainer, Signal: hostenv.SignalContainerenv, Engine: hostenv.EnginePodman,
		}},
		{"overlay root", fstest.MapFS{"proc/self/mountinfo": {Data: []byte("1 0 0:50 / / rw - overlay overlay rw\n")}}, &hostenv.Env{
			Kind: hostenv.KindContainer, Signal: hostenv.SignalOverlayRoot, Engine: hostenv.Unidentified,
		}},
		{"wsl", fstest.MapFS{
			"proc/version":        {Data: []byte("Linux version 5.15.153.1-microsoft-standard-WSL2")},
			"proc/self/mountinfo": {Data: []byte(wslMountinfo)},
		}, &hostenv.Env{Kind: hostenv.KindWSL, Signal: hostenv.SignalProcVersion, Drives: []string{"C:", "D:"}}},
		{"wsl interop", fstest.MapFS{"proc/sys/fs/binfmt_misc/WSLInterop": {}}, &hostenv.Env{Kind: hostenv.KindWSL, Signal: hostenv.SignalWSLInterop}},
		{"vm", fstest.MapFS{
			"sys/class/dmi/id/sys_vendor":   {Data: []byte("QEMU\n")},
			"sys/class/dmi/id/product_name": {Data: []byte("Standard PC (Q35 + ICH9, 2009)\n")},
		}, &hostenv.Env{Kind: hostenv.KindVirtualMachine, Signal: hostenv.SignalDMI, VMType: "qemu"}},
		{"physical", fstest.MapFS{
			"sys/class/dmi/id/sys_vendor":   {Data: []byte("LENOVO\n")},
			"sys/class/dmi/id/product_name": {Data: []byte("ThinkPad X1\n")},
		}, nil},
	} {
		if got := hostenv.DetectLinux(c.fs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestVMTypeOf(t *testing.T) {
	for _, c := range [][3]string{
		{"Microsoft Corporation", "Virtual Machine", "hyper_v"},
		{"Microsoft Corporation", "Surface Laptop 5", ""},
		{"VMware, Inc.", "VMware Virtual Platform", "vmware"},
		{"innotek GmbH", "VirtualBox", "virtualbox"},
		{"Amazon EC2", "m5.large", "amazon_ec2"},
		{"Google", "Google Compute Engine", "google"},
		{"Parallels Software International Inc.", "Parallels Virtual Platform", "parallels"},
		{"Dell Inc.", "XPS 13", ""},
	} {
		if got := hostenv.VMTypeOf(c[0], c[1]); got != c[2] {
			t.Errorf("%q %q: %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestTestEnvironment(t *testing.T) {
	var none *hostenv.Env
	if none.TestEnvironment() || (&hostenv.Env{Kind: hostenv.KindWSL}).TestEnvironment() {
		t.Error("no environment or WSL gets the test-environment tag")
	}
	for _, k := range []string{hostenv.KindContainer, hostenv.KindVirtualMachine, hostenv.KindWindowsSandbox} {
		if !(&hostenv.Env{Kind: k}).TestEnvironment() {
			t.Errorf("%s: no tag", k)
		}
	}
}
