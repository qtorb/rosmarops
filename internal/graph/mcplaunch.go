package graph

import (
	"regexp"
	"strings"

	"github.com/qtorb/rosmarops/internal/platform"
)

// Unpinned launches (BV-274, mcp_unpinned_launch). A local MCP server
// started by a launcher that fetches its code when it starts (npx, bunx,
// pnpm dlx, uvx, pipx run, docker run) runs whatever that package or image
// is at that moment unless the configuration names an exact version:
// pkg@1.2.3, pkg==1.2.3, or for an image, image@sha256:…. No version, a
// range (^, ~, >=, *, x) or a tag (latest, next; and for Docker, any tag)
// is unpinned. The arguments are parsed in memory and never stored: the
// mark rides on the mcp_server node, like Malicious, with only the launcher
// and the package or image name.

// UnpinnedLaunch is how an unpinned server starts: its launcher (npx, bunx,
// pnpm dlx, uvx, pipx run, docker run) and the package or image name,
// without any version or tag.
type UnpinnedLaunch struct {
	Launcher string
	Package  string
}

// exactVersion is a version that names one release: digits and dots, with
// an optional pre-release or build part (1.2.3, 1.2.3-rc.1, 2024.11.0+b1).
var exactVersion = regexp.MustCompile(`^v?[0-9]+(\.[0-9]+)*([-+][0-9A-Za-z.-]+)?$`)

// isExactVersion reports a version that names one release: not a range nor
// a tag. A version with x or * in a part (1.x, 1.2.*) is a range.
func isExactVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || strings.ContainsAny(v, "^~<>=*| ") {
		return false
	}
	for _, part := range strings.Split(strings.SplitN(v, "-", 2)[0], ".") {
		if strings.EqualFold(part, "x") {
			return false
		}
	}
	return exactVersion.MatchString(v)
}

// launchArgs strips a Windows command shell wrapper (cmd /c npx …) and
// returns the launcher's base name and its arguments.
func launchArgs(srv platform.MCPServer) (string, []string) {
	cmd := strings.TrimSuffix(strings.ToLower(baseName(srv.Command)), ".exe")
	cmd = strings.TrimSuffix(strings.TrimSuffix(cmd, ".cmd"), ".ps1")
	args := srv.Args
	if cmd == "cmd" && len(args) > 1 && (strings.EqualFold(args[0], "/c") || strings.EqualFold(args[0], "/k")) {
		return launchArgs(platform.MCPServer{Command: args[1], Args: args[2:]})
	}
	return cmd, args
}

// UnpinnedLaunchOf reports srv's unpinned launch, ok false when it is not
// started by one of the six launchers or names an exact version.
func UnpinnedLaunchOf(srv platform.MCPServer) (UnpinnedLaunch, bool) {
	if strings.TrimSpace(srv.Command) == "" {
		return UnpinnedLaunch{}, false // a remote server: no local launch
	}
	cmd, args := launchArgs(srv)
	switch cmd {
	case "npx", "bunx":
		return packageLaunch(cmd, args, npmFlagValue, npmPackageFlags, npmSpec)
	case "pnpm":
		if len(args) > 0 && strings.EqualFold(args[0], "dlx") {
			return packageLaunch("pnpm dlx", args[1:], npmFlagValue, npmPackageFlags, npmSpec)
		}
	case "uvx":
		return packageLaunch(cmd, args, uvFlagValue, uvPackageFlags, pySpec)
	case "pipx":
		if len(args) > 0 && strings.EqualFold(args[0], "run") {
			return packageLaunch("pipx run", args[1:], pipxFlagValue, pipxPackageFlags, pySpec)
		}
	case "docker":
		if len(args) > 1 && strings.EqualFold(args[0], "container") && strings.EqualFold(args[1], "run") {
			return dockerLaunch(args[2:])
		}
		if len(args) > 0 && strings.EqualFold(args[0], "run") {
			return dockerLaunch(args[1:])
		}
	}
	return UnpinnedLaunch{}, false
}

// Flags that take a value, per launcher, and those whose value is the
// package spec itself.
var (
	npmFlagValue     = map[string]bool{"-p": true, "--package": true, "-c": true, "--call": true, "--registry": true, "--cache": true, "--userconfig": true}
	npmPackageFlags  = map[string]bool{"-p": true, "--package": true}
	uvFlagValue      = map[string]bool{"--from": true, "--with": true, "-w": true, "--with-requirements": true, "--with-editable": true, "--python": true, "-p": true, "--index": true, "--index-url": true, "-i": true, "--extra-index-url": true, "--default-index": true, "--directory": true, "--project": true, "--cache-dir": true, "--config-file": true, "--constraints": true, "-c": true, "--overrides": true}
	uvPackageFlags   = map[string]bool{"--from": true}
	pipxFlagValue    = map[string]bool{"--spec": true, "--python": true, "--pip-args": true, "--index-url": true, "-i": true}
	pipxPackageFlags = map[string]bool{"--spec": true}
)

// packageLaunch finds the package spec: the value of a package flag
// (--package, --from, --spec) when there is one, else the first positional
// argument; spec splits it into name and version.
func packageLaunch(launcher string, args []string, takesValue, packageFlag map[string]bool, spec func(string) (string, string)) (UnpinnedLaunch, bool) {
	var pkgSpec string
	for i := 0; i < len(args); i++ {
		a := strings.TrimSpace(args[i])
		if a == "" {
			continue
		}
		if a == "--" {
			if pkgSpec == "" && i+1 < len(args) {
				pkgSpec = args[i+1]
			}
			break
		}
		if strings.HasPrefix(a, "-") {
			name, value, inline := strings.Cut(a, "=")
			if !inline && takesValue[name] && i+1 < len(args) {
				value = args[i+1]
				i++
			}
			if packageFlag[name] && pkgSpec == "" {
				pkgSpec = value
			}
			continue
		}
		if pkgSpec == "" {
			pkgSpec = a
		}
		break // the first positional is the package (or, with a package flag, the command)
	}
	if pkgSpec == "" || looksLikePath(pkgSpec) {
		return UnpinnedLaunch{}, false
	}
	name, version := spec(pkgSpec)
	if name == "" || isExactVersion(version) {
		return UnpinnedLaunch{}, false
	}
	return UnpinnedLaunch{Launcher: launcher, Package: name}, true
}

// looksLikePath is a spec that is a local path, a URL or a git source:
// not a registry package, so not this rule's.
func looksLikePath(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "~") || strings.HasPrefix(s, "git+") || strings.HasPrefix(s, "file:") ||
		strings.HasPrefix(s, "github:") || (len(s) > 1 && s[1] == ':') || strings.Contains(s, `\`)
}

// npmSpec splits name@version (a leading @ is the scope).
func npmSpec(s string) (name, version string) {
	if i := strings.LastIndexByte(s, '@'); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// pySpec splits name==version, name@version or a name with another
// specifier (>=, ~=, <), whose version is then a range. Extras are dropped
// from the name.
func pySpec(s string) (name, version string) {
	if i := strings.Index(s, "=="); i > 0 {
		name, version = s[:i], s[i+2:]
	} else if i := strings.LastIndexByte(s, '@'); i > 0 {
		name, version = s[:i], s[i+1:]
	} else if i := strings.IndexAny(s, "<>~=!"); i > 0 {
		name, version = s[:i], s[i:]
	} else {
		name = s
	}
	if j := strings.IndexByte(name, '['); j > 0 {
		name = name[:j]
	}
	return strings.TrimSpace(name), strings.TrimSpace(version)
}

// dockerFlagValue are the docker run flags that take a value (docker run
// --help): the image is the first argument that is neither a flag nor one
// of these values.
var dockerFlagValue = map[string]bool{
	"-a": true, "--attach": true, "--add-host": true, "--annotation": true, "--blkio-weight": true, "--cap-add": true, "--cap-drop": true,
	"--cgroup-parent": true, "--cgroupns": true, "--cidfile": true, "--cpu-period": true, "--cpu-quota": true, "--cpu-shares": true, "-c": true,
	"--cpus": true, "--cpuset-cpus": true, "--cpuset-mems": true, "--device": true, "--device-cgroup-rule": true, "--dns": true,
	"--dns-option": true, "--dns-search": true, "--domainname": true, "--entrypoint": true, "-e": true, "--env": true, "--env-file": true,
	"--expose": true, "--gpus": true, "--group-add": true, "--health-cmd": true, "--health-interval": true, "--health-retries": true,
	"--health-start-period": true, "--health-timeout": true, "-h": true, "--hostname": true, "--ip": true, "--ip6": true, "--ipc": true,
	"--isolation": true, "-l": true, "--label": true, "--label-file": true, "--link": true, "--log-driver": true, "--log-opt": true,
	"--mac-address": true, "-m": true, "--memory": true, "--memory-reservation": true, "--memory-swap": true, "--mount": true,
	"--name": true, "--network": true, "--net": true, "--network-alias": true, "--pid": true, "--pids-limit": true, "--platform": true,
	"-p": true, "--publish": true, "--pull": true, "--restart": true, "--runtime": true, "--security-opt": true, "--shm-size": true,
	"--stop-signal": true, "--stop-timeout": true, "--storage-opt": true, "--sysctl": true, "--tmpfs": true, "--ulimit": true,
	"-u": true, "--user": true, "--userns": true, "--uts": true, "-v": true, "--volume": true, "--volumes-from": true, "-w": true,
	"--workdir": true,
}

// dockerLaunch is docker run's image: unpinned unless it names a digest.
func dockerLaunch(args []string) (UnpinnedLaunch, bool) {
	for i := 0; i < len(args); i++ {
		a := strings.TrimSpace(args[i])
		if a == "" {
			continue
		}
		if strings.HasPrefix(a, "-") {
			name, _, inline := strings.Cut(a, "=")
			if !inline && dockerFlagValue[name] && i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.Contains(a, "@sha256:") {
			return UnpinnedLaunch{}, false
		}
		return UnpinnedLaunch{Launcher: "docker run", Package: imageName(a)}, true
	}
	return UnpinnedLaunch{}, false
}

// imageName is an image reference without its tag: the part after the last
// slash may hold :tag; a registry's :port comes before a slash.
func imageName(ref string) string {
	if i := strings.LastIndexByte(ref, ':'); i > strings.LastIndexByte(ref, '/') {
		return ref[:i]
	}
	return ref
}

// MarkUnpinned sets Unpinned on the mcp_server node of every server whose
// launch is unpinned. It mutates g in place.
func MarkUnpinned(g *Graph, servers []platform.MCPServer) {
	if g == nil {
		return
	}
	byID := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		byID[n.ID] = i
	}
	for _, srv := range servers {
		if !srv.LoadsMCP {
			continue
		}
		idx, ok := byID[serverID(srv)]
		if !ok {
			continue
		}
		if u, ok := UnpinnedLaunchOf(srv); ok {
			mark := u
			g.Nodes[idx].Unpinned = &mark
		}
	}
}
