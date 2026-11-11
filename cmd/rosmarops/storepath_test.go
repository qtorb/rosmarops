package main

import "testing"

func TestStoreAliasExe(t *testing.T) {
	const lad = `C:\Users\albert\AppData\Local`
	alias := lad + `\Microsoft\WindowsApps\rosmarops.exe`

	cases := []struct {
		name string
		real string
		lad  string
		want string
	}{
		{
			name: "store install maps to the alias",
			real: `C:\Program Files\WindowsApps\AlbertGarciaPujadas.RosmarOps_0.11.0.0_x64__2a9trf99pxwv0\rosmarops.exe`,
			lad:  lad,
			want: alias,
		},
		{
			name: "case-insensitive WindowsApps",
			real: `C:\Program Files\windowsapps\Pkg_1.0.0.0_arm64__abc\rosmarops.exe`,
			lad:  lad,
			want: alias,
		},
		{
			name: "standalone exe unchanged",
			real: `C:\Users\albert\Downloads\rosmarops.exe`,
			lad:  lad,
			want: `C:\Users\albert\Downloads\rosmarops.exe`,
		},
		{
			name: "no LOCALAPPDATA leaves the real path (fail-safe)",
			real: `C:\Program Files\WindowsApps\Pkg\rosmarops.exe`,
			lad:  "",
			want: `C:\Program Files\WindowsApps\Pkg\rosmarops.exe`,
		},
		{
			name: "linux/mac path unchanged",
			real: "/home/albert/.local/bin/rosmarops",
			lad:  "",
			want: "/home/albert/.local/bin/rosmarops",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := storeAliasExe(c.real, c.lad); got != c.want {
				t.Fatalf("storeAliasExe(%q, %q) = %q, want %q", c.real, c.lad, got, c.want)
			}
		})
	}
}
