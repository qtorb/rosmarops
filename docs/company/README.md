# Company install scripts

Drafts for installing RosmarOps on company-managed computers. They are
**untested until the pilot**: the `SIN PROBAR` line at the top of each file
means "untested" and stays there until the pilot confirms them.

| File | What it does |
|---|---|
| `install-windows.ps1` | Run as an administrator (for example by Fleet after it downloads the `.exe`; `$env:INSTALLER_PATH` points to it). Copies the binary to `%ProgramFiles%\RosmarOps\`, writes `company.json` and `binaries.sha256` to `%ProgramData%\RosmarOps\`, restricts both folders so only Administrators and SYSTEM can write (Users read), and registers one scheduled task for all users, weekly and at log-on, not elevated, that runs `rosmarops run --unattended`. |
| `uninstall-windows.ps1` | Removes the scheduled task and both folders. It never touches anyone's `~\.rosmarops`: that is each person's data. |
| `install-linux.sh` | Run as root; `$INSTALLER_PATH` is the folder that holds `rosmarops-linux-amd64`. Installs `/usr/local/bin/rosmarops`, writes `/etc/rosmarops/company.json` and `/etc/rosmarops/binaries.sha256` (root, 0644), and enables a weekly systemd user timer for every user (`systemctl --global`), with the same unit names as the timer a person creates, so a person's own timer takes precedence. |

`$version` (Windows) and `VERSION` (Linux) must be set to the version being
installed: it goes into `binaries.sha256` next to the binary's SHA-256, and
RosmarOps only accepts a binary on a person's first run when both match.

## How CI exercises them

The `company-scripts-linux` and `company-scripts-windows` jobs in
`.github/workflows/ci.yml` build the binary with `-X main.version=0.11.2`
(the scripts' version), run the install script as an administrator, and print
and check what it left: owners and permissions, the two files, the hash in
`binaries.sha256` against the installed binary, and the timer or the
scheduled task. Then, as a person with no `config.json` of their own (a new
`fakeuser` on Linux; the runner's user, or a fresh profile folder, on Windows),
they run `rosmarops run --unattended` and check that the summary
`~/.rosmarops/reports/data/rosmarops-summary.json` is one JSON line with
`config_source` `company`, `run` `complete` and `binary` `first_run`, that no
`config.json` was created and that the company files did not change. Linux
runs a second review and checks `binary` `unchanged`; Windows runs the
uninstall script and checks that the folders and the task are gone. The
review's exit code is printed, not required to be 0. Neither job runs the
scheduled task or the timer themselves.
