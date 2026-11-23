#!/usr/bin/env python3
"""journey: the journey tests R1, R2 and R3 (journeys.yml), on Linux, macOS
and Windows, against a candidate RosmarOps binary.

    journey.py r1 --bin CAND --version X --tools DIR --out DIR
    journey.py r2 --bin CAND --prev PREV --version X --tools DIR --out DIR
    journey.py r3 --bin CAND --version X --tools DIR --out DIR

Each journey runs on a test computer that tests/journeys/makehome builds in
its own home folder (HOME, USERPROFILE, APPDATA and LOCALAPPDATA point there;
the runner's own folders are never used), drives RosmarOps as a person would
(tests/journeys/drive) and runs the fixed checks (tests/journeys/check). Every
check is one line in --out/results.json and in the job summary. The console of
every step, its report pages and what capture.py needs are kept under --out.

A check whose name contains one of the comma-separated pieces of
JOURNEY_EXPECTED_RED and fails is reported as expected red, with
JOURNEY_EXPECTED_RED_WHY, and does not fail the journey; one that passes says
it can leave the list.

Only the Python standard library. A test tool, never part of RosmarOps.
"""

import argparse
import json
import os
import platform
import re
import shutil
import stat
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
CATALOG = os.path.join(ROOT, "internal", "catalog", "messages")
STEPS = os.path.join(ROOT, "tests", "journeys", "steps")
SYSTEM = {"Linux": "linux", "Darwin": "macos", "Windows": "windows"}[platform.system()]
EXE = ".exe" if SYSTEM == "windows" else ""
# Only on Linux does the locale choose RosmarOps's language: macOS and Windows
# follow the system's display language, English on the runners.
LOCALE_LANG = SYSTEM == "linux"


class Journey:
    def __init__(self, args, name):
        self.args = args
        self.name = name
        self.out = os.path.abspath(args.out)
        os.makedirs(self.out, exist_ok=True)
        self.results = []
        self.expected = [s.strip() for s in os.environ.get("JOURNEY_EXPECTED_RED", "").split(",") if s.strip()]
        self.why = os.environ.get("JOURNEY_EXPECTED_RED_WHY", "")
        self.captures = []  # what capture.py draws: [{"kind", "path", "step", "lang"}]
        self.installed = []  # program paths outside the test home that may appear (the MSIX alias)
        self.homes = []  # every test computer made, for the usage check at the end
        self.work = tempfile.mkdtemp(prefix=f"journey-{name}-", dir=os.environ.get("JOURNEY_WORK"))
        self.stub = os.path.join(self.work, "stub")
        os.makedirs(self.stub)
        if SYSTEM == "linux":
            # A desktop that opens the report: record the path RosmarOps asks
            # to open, so the snap check opens exactly that one.
            with open(os.path.join(self.stub, "xdg-open"), "w") as f:
                f.write('#!/bin/sh\nprintf "%s\\n" "$1" >> "$JOURNEY_OPENED"\nexit 0\n')
            os.chmod(os.path.join(self.stub, "xdg-open"), 0o755)

    # --- results ------------------------------------------------------------
    def check(self, name, ok, detail=""):
        expected = any(e in name for e in self.expected)
        if ok:
            mark, status = "✅", "ok"
            if expected:
                detail = (detail + " · " if detail else "") + "passes: it can leave JOURNEY_EXPECTED_RED"
        elif expected:
            mark, status = "🟠", "expected_red"
            detail = (detail + " · " if detail else "") + "expected red: " + self.why
        else:
            mark, status = "❌", "fail"
        self.results.append({"journey": self.name, "system": SYSTEM, "check": name, "status": status, "detail": detail})
        line = f"- {mark} {self.name} · {SYSTEM} · {name}" + (f": {detail}" if detail else "")
        print(line, flush=True)
        summary = os.environ.get("GITHUB_STEP_SUMMARY")
        if summary:
            with open(summary, "a", encoding="utf-8") as f:
                f.write(line + "\n")
        return ok

    def failed(self):
        return any(r["status"] == "fail" for r in self.results)

    def finish(self):
        for home in self.homes:
            self.usage_checks(home)
        with open(os.path.join(self.out, "results.json"), "w", encoding="utf-8") as f:
            json.dump({"journey": self.name, "system": SYSTEM, "version": self.args.version,
                       "passed": not self.failed(), "results": self.results, "captures": self.captures}, f, indent=1)
        return 1 if self.failed() else 0

    def usage_checks(self, home):
        """No journey may turn usage statistics on in its test computer
        (BV-271): its runs would be counted as people's. The state must not
        record them on, and no usage package may have been built."""
        label = os.path.basename(home)
        enabled = False
        try:
            with open(os.path.join(home, ".rosmarops", "state.json"), encoding="utf-8") as f:
                enabled = bool((json.load(f).get("usage") or {}).get("enabled"))
        except (OSError, ValueError):
            pass
        package = [os.path.join(d, "data", "rosmarops-usage-last.json")
                   for d in (os.path.join(home, "RosmarOps"), os.path.join(home, ".rosmarops", "reports"))]
        built = [p for p in package if os.path.exists(p)]
        self.check(f"{label}: usage statistics stay off", not enabled and not built,
                   "state.json records them on" if enabled else (f"a usage package was built: {built[0]}" if built else ""))

    # --- the test computer --------------------------------------------------
    def make_home(self, label, medium=0, paperclip=False):
        home = os.path.join(self.work, label)
        self.homes.append(home)
        manifest = os.path.join(self.work, label + "-manifest.json")
        cmd = [self.tool("makehome"), "-home", home, "-manifest", manifest, "-fixture",
               os.path.join(ROOT, "testdata", "sample-home"), "-medium", str(medium), "-large", "0"]
        if paperclip:
            cmd.append("-paperclip")
        subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL)
        return home

    def paperclip_checks(self, rep, label):
        """R1's fake Paperclip (makehome -paperclip): the report has its
        orchestrator findings, its secrets are credentials known by their
        path, and none of its fake secret values is in any output."""
        if rep is None:
            return
        try:
            with open(os.path.join(rep, "data", "report.v2.json"), encoding="utf-8") as f:
                found = {x.get("rule") for x in json.load(f).get("findings", [])}
        except (OSError, ValueError) as e:
            self.check(f"{label}: Paperclip findings", False, f"report.v2.json not read: {e}")
            return
        for rule in ("orchestrator_skips_approvals", "orchestrator_vulnerable_version"):
            self.check(f"{label}: Paperclip: {rule}", rule in found, "" if rule in found else f"rules found: {sorted(found)}")
        leaked = []
        for root, _, files in os.walk(rep):
            for name in files:
                try:
                    with open(os.path.join(root, name), encoding="utf-8", errors="replace") as f:
                        text = f.read()
                except OSError:
                    continue
                for value in PAPERCLIP_FAKE_SECRETS:
                    if value in text:
                        leaked.append(name)
        self.check(f"{label}: Paperclip: no secret value in the reports", not leaked, ", ".join(sorted(set(leaked))))

    def install(self, home, src):
        """The program as a person keeps it: downloaded into their own
        Downloads folder. The report names this path (the Claude Code hook
        command), so it must be inside the test home, never the checkout."""
        binary = os.path.join(home, "Downloads", "rosmarops" + EXE)
        os.makedirs(os.path.dirname(binary), exist_ok=True)
        shutil.copyfile(src, binary)
        os.chmod(binary, 0o755)
        return binary

    def env(self, home, lang):
        e = {k: v for k, v in os.environ.items() if not k.startswith(("XDG_", "LC_", "JOURNEY_EXPECTED"))}
        e.update({"HOME": home, "USERPROFILE": home, "APPDATA": os.path.join(home, "AppData", "Roaming"),
                  "LOCALAPPDATA": os.path.join(home, "AppData", "Local"), "TERM": "xterm",
                  "JOURNEY_OPENED": os.path.join(self.work, "opened.txt")})
        if SYSTEM != "windows":
            locale = "es_ES.UTF-8" if lang == "es" else "C.UTF-8"
            e.update({"LANG": locale, "LC_ALL": locale})
        e["PATH"] = self.stub + os.pathsep + e.get("PATH", "")
        return e

    def tool(self, name):
        return os.path.join(self.args.tools, name + EXE)

    # --- running ------------------------------------------------------------
    def drive(self, steps, lang, argv, home, label, yes=None, windows=()):
        """Run argv in a terminal through the steps; the console is kept as
        label.txt. windows names the RosmarOps windows this command opens on
        Windows: their self-test hooks are set, so each real window opens and
        answers itself (a journey cannot click). Returns (ok, why)."""
        if yes is not None:
            with open(os.path.join(STEPS, steps), encoding="utf-8") as f:
                text = f.read().replace("@YES@", yes)
            steps = os.path.join(self.work, label + ".steps")
            with open(steps, "w", encoding="utf-8") as f:
                f.write(text)
        else:
            steps = os.path.join(STEPS, steps)
        console = os.path.join(self.out, label + ".txt")
        if SYSTEM == "windows":
            cmd = [self.tool("conpty"), "-steps", steps, "-catalog", CATALOG, "-lang", lang, "-text", console, "--"] + argv
        else:
            cmd = [sys.executable, os.path.join(ROOT, "tests", "journeys", "drive", "drive.py"), "--steps", steps,
                   "--catalog", CATALOG, "--lang", lang, "--text", console, "--"] + argv
        env = self.env(home, lang)
        if SYSTEM == "windows":
            for w in windows:
                env[SELFTEST_HOOK[w]] = "1"
            # The usage statistics window (BV-286) opens only in a published
            # version; whenever it does, it answers itself as «later», so no
            # journey ever waits for it.
            env[SELFTEST_HOOK["usage"]] = "1"
        p = subprocess.run(cmd, env=env, cwd=home, capture_output=True, text=True,
                           encoding="utf-8", errors="replace")
        why = (p.stderr or p.stdout).strip().splitlines()
        if p.returncode != 0:
            # The last screen, so the run's log shows what it was waiting for.
            try:
                with open(console, encoding="utf-8", errors="replace") as f:
                    tail = [ln for ln in f.read().splitlines() if ln.strip()][-12:]
            except OSError:
                tail = []
            print(f"  {label}: the console's last lines:")
            for ln in tail:
                print("    | " + ln)
        return p.returncode == 0, (why[0] if why else "")

    def plain(self, argv, home, lang, label):
        """Run argv without a terminal (standard input empty); returns the exit
        code, and the output is kept as label.txt."""
        p = subprocess.run(argv, env=self.env(home, lang), cwd=home, stdin=subprocess.DEVNULL,
                           capture_output=True, text=True, encoding="utf-8", errors="replace")
        with open(os.path.join(self.out, label + ".txt"), "w", encoding="utf-8") as f:
            f.write(p.stdout + p.stderr)
        return p.returncode

    def run_check(self, *argv):
        p = subprocess.run([self.tool("check")] + list(argv), capture_output=True, text=True, cwd=ROOT,
                           encoding="utf-8", errors="replace")
        lines = [l for l in (p.stdout + p.stderr).splitlines() if l.startswith("FAIL") or "check:" in l]
        return p.returncode, "; ".join(lines[:5])

    # --- what a review left -------------------------------------------------
    def window_checks(self, home, console, label, windows):
        """Windows: each window the command opened logged its self-test
        «shown» line, in the run's log (data/last-run-log.txt) or, for a command
        without a run log, on the console with its other log lines."""
        if SYSTEM != "windows":
            return
        texts = []
        log_lines = []
        rep = self.reports_dir(home)
        # The run log lives in the reports folder's data/ (report.DataDir).
        for f in ([os.path.join(rep, "data", "last-run-log.txt")] if rep else []) + [console]:
            try:
                with open(f, encoding="utf-8", errors="replace") as fh:
                    texts.append(fh.read())
            except OSError:
                texts.append("")
        if rep:
            log_lines = texts[0].splitlines() or ["(the run log is empty or missing)"]
        lines = "\n".join(texts).splitlines()
        for w in windows:
            shown = any("window self-test: shown" in ln and f"window={w}" in ln for ln in lines)
            self.check(f"{label}: the {w} window opened (its self-test)", shown,
                       "" if shown else "no «window self-test: shown» line for it in the run's log")
            if not shown:
                print(f"  {label}: the run log's last lines:" + ("" if rep else " (no reports folder)"))
                for ln in log_lines[-25:]:
                    print("    | " + ln)

    def reports_dir(self, home):
        for d in (os.path.join(home, "RosmarOps"), os.path.join(home, ".rosmarops", "reports")):
            if os.path.exists(os.path.join(d, "report.html")):
                return d
        return None

    def review_checks(self, home, lang, console, label, version_check=True):
        """The fixed checks on what a review left: its report exists, no
        placeholder or (Spanish run) English sentence in the console or the
        page, the binary's version everywhere, no runner path or host name."""
        rep = self.reports_dir(home)
        if not self.check(f"{label}: report written", rep is not None):
            return None
        page = os.path.join(rep, f"report.{lang}.html")
        code, why = self.run_check("text", "-catalog", CATALOG, "-lang", lang, console, page)
        self.check(f"{label}: no unfilled text" + (" and no English" if lang == "es" else ""), code == 0, why)
        if version_check:
            vout = os.path.join(self.out, label + "-version.txt")
            with open(vout, "w") as f:
                f.write(subprocess.run([self.args.bin, "--version"], capture_output=True, text=True).stdout)
            code, why = self.run_check("version", "-want", self.args.version, "-version-output", vout,
                                       "-v2", os.path.join(rep, "data", "report.v2.json"), "-html", page)
            self.check(f"{label}: version {self.args.version} in the report", code == 0, why)
        forbid = [p for p in (os.path.expanduser("~"), os.environ.get("GITHUB_WORKSPACE"),
                              os.environ.get("RUNNER_TEMP"), os.environ.get("RUNNER_TOOL_CACHE")) if p]
        allow = [os.path.dirname(b) for b in self.installed]
        code, why = self.run_check("leaks", "-home", home, "-allow", ",".join(allow), "-forbid", ",".join(forbid), console,
                                   os.path.join(rep, "report.es.html"), os.path.join(rep, "report.en.html"))
        self.check(f"{label}: no runner path or machine name", code == 0, why)
        return rep

    def snap_gate(self, label):
        """Linux: the report RosmarOps opened is readable from inside the
        Firefox snap (a real open, which AppArmor decides)."""
        opened = os.path.join(self.work, "opened.txt")
        try:
            path = open(opened).read().split("\n")[-2]
        except (OSError, IndexError):
            return self.check(f"{label}: Firefox (snap) opens the report", False, "RosmarOps opened nothing")
        if path.startswith("file://"):
            path = path[len("file://"):]
        if shutil.which("snap") is None:
            return self.check(f"{label}: Firefox (snap) opens the report", False, "no snap on this machine")
        p = subprocess.run(["snap", "run", "--shell", "firefox", "-c", f"head -c1 '{path}' >/dev/null"],
                           capture_output=True, text=True)
        return self.check(f"{label}: Firefox (snap) opens the report", p.returncode == 0,
                          path.replace(os.path.expanduser("~"), "~") + ("" if p.returncode == 0 else ": " + p.stderr.strip()))

    def keep_report(self, rep, step, lang, console):
        """Copy the report pages for the captures (a folder a snap browser can
        read) and list them, with the step's console."""
        for l in ("es", "en"):
            src = os.path.join(rep, f"report.{l}.html")
            if os.path.exists(src):
                dst = os.path.join(self.out, f"{step}-report.{l}.html")
                shutil.copyfile(src, dst)
                self.captures.append({"kind": "report", "path": dst, "step": step, "lang": l})
        self.captures.append({"kind": "console", "path": console, "step": step, "lang": lang})


def remove_tree(path):
    """shutil.rmtree that also deletes read-only files: on Windows git's
    object files are read-only and a plain delete is refused."""
    def again(func, p, _):
        os.chmod(p, stat.S_IWRITE)
        func(p)
    if sys.version_info >= (3, 12):
        shutil.rmtree(path, onexc=again)
    else:
        shutil.rmtree(path, onerror=again)


def install_msix(folder):
    """Windows: trust the package's test certificate and install (or update
    to) the MSIX test package in folder; returns the alias that runs it."""
    msix = [f for f in os.listdir(folder) if f.endswith("-test.msix")][0]
    cer = [f for f in os.listdir(folder) if f.endswith("-test.cer")][0]
    script = (f"$ErrorActionPreference='Stop'; Import-Certificate -FilePath '{os.path.join(folder, cer)}' "
              f"-CertStoreLocation Cert:\\LocalMachine\\TrustedPeople | Out-Null; "
              f"Add-AppxPackage -Path '{os.path.join(folder, msix)}' -ForceUpdateFromAnyVersion")
    subprocess.run(["powershell", "-NoProfile", "-Command", script], check=True)
    return os.path.join(os.environ["LOCALAPPDATA"], "Microsoft", "WindowsApps", "rosmarops.exe")


def red_report(home):
    """The red report an integrity stop writes: report-<date>__RED__...json in
    the reports folder, with the binary_hash_changed finding."""
    for d in (os.path.join(home, "RosmarOps"), os.path.join(home, ".rosmarops", "reports")):
        if not os.path.isdir(d):
            continue
        for name in os.listdir(d):
            if "__RED__" in name and name.endswith(".json"):
                try:
                    with open(os.path.join(d, name), encoding="utf-8") as f:
                        r = json.load(f)
                except (OSError, ValueError):
                    continue
                if any(x.get("category") == "binary_hash_changed" for x in r.get("findings", [])):
                    return True
    return False


def r1(args):
    j = Journey(args, "R1 MSIX" if args.msix_cand else "R1")
    if args.msix_cand:
        binary = install_msix(args.msix_cand)
        j.installed.append(binary)
    legs = ["es", "en"] if LOCALE_LANG else ["en"]
    for lang in legs:
        label = f"r1-{lang}"
        home = j.make_home(label, paperclip=True)
        if not args.msix_cand:
            binary = j.install(home, args.bin)
        r1_windows = usage_windows(args.version)
        ok, why = j.drive("r1.steps", lang, [binary], home, label, windows=r1_windows)
        j.check(f"{label}: no arguments ends without an unplanned question", ok, why)
        j.window_checks(home, os.path.join(j.out, label + ".txt"), label, r1_windows)
        rep = j.review_checks(home, lang, os.path.join(j.out, label + ".txt"), label)
        j.paperclip_checks(rep, label)
        if SYSTEM == "linux":
            j.snap_gate(label)
        if rep:
            j.keep_report(rep, "first-run", lang, os.path.join(j.out, label + ".txt"))
        if not LOCALE_LANG:
            # The Spanish leg: on these systems the language does not follow
            # the locale, so it is asked for.
            label = "r1-es"
            ok, why = j.drive("r1-run.steps", "es", [binary, "run", "--lang", "es"], home, label)
            j.check(f"{label}: run --lang es ends without an unplanned question", ok, why)
            rep = j.review_checks(home, "es", os.path.join(j.out, label + ".txt"), label)
            if rep:
                j.keep_report(rep, "run-lang-es", "es", os.path.join(j.out, label + ".txt"))
    return j.finish()


# The RosmarOps windows a command opens on Windows, and each window's self-test
# hook. A journey cannot click a window, so on Windows the hook is set: the real
# window opens and answers itself («Not now», nothing saved), and the window's
# «shown» line in the run's log proves it opened. A person's click is not
# tested here.
# The fake secret values of makehome -paperclip (BV-264): they must never be
# read, so never in any output.
PAPERCLIP_FAKE_SECRETS = ("FAKEpaperclipMASTERkeyFORjourneysONLY0000", "FAKEpaperclipJWTsecretFORjourneysONLY0000")

# A published version (X.Y.Z) asks about usage statistics; a journeys build
# (X.Y.Z-journeys.N, every pull request) never does (BV-284), so in a pull
# request's journeys the usage statistics window never opens.
PUBLISHED = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")


def usage_windows(version):
    """The usage statistics window, when this version opens it."""
    return ("usage",) if PUBLISHED.match(version or "") else ()


# R1, the first review (BV-286): the double click shows only the usage
# statistics window (and the problem list, if something failed, which a
# journey's computer does not have): no connect offer and no findings window.
# R2, after the update: the previous version already reviewed this computer,
# so no finding is new; the double click asks about usage statistics first
# while there is no answer (the window answers itself «later», and TextVersion
# 2 asks again whoever answered text 1), then offers to connect; `findings`
# always opens its window.
def r2_windows(version):
    return {"no arguments": usage_windows(version) + ("connect",), "findings": ("findings",)}


SELFTEST_HOOK = {"connect": "ROSMAROPS_CONNECT_WINDOW_SELFTEST", "findings": "ROSMAROPS_FINDINGS_WINDOW_SELFTEST",
                 "usage": "ROSMAROPS_USAGE_WINDOW_SELFTEST"}


# R2's commands after the update: (label, argv, kind, how to judge it).
#   ask:   asks whether the person installed this version, takes the answer,
#          finishes with 0;
#   abort: without a terminal, stops with 2 and a red report;
#   exit0: finishes with 0 without asking.
# With the MSIX package and a candidate that is a newer released version, the
# update is a Microsoft Store update (store_update): «ask» and «abort» rows
# instead finish with 0 without asking, and the state records the change as
# accepted by the Store.
R2_ROWS = [
    ("no arguments", [], "ask"),
    ("run", ["run"], "ask"),
    ("run --unattended", ["run", "--unattended"], "abort"),
    ("usage", ["usage"], "ask"),
    ("status", ["status"], "ask"),
    ("findings", ["findings"], "ask"),
    ("problems ask", ["problems", "ask"], "ask"),
    ("baseline", ["baseline"], "exit0"),
    ("connect --help", ["connect", "--help"], "exit0"),
    ("--version", ["--version"], "exit0"),
]


def semver_core(v):
    """The numeric core of a strict X.Y.Z[-pre] version, or None."""
    core, _, pre = v.partition("-")
    parts = core.split(".")
    if len(parts) != 3 or not all(p.isdigit() for p in parts):
        return None, pre
    return tuple(int(p) for p in parts), pre


def store_update(msix, version, prev_version):
    """Whether RosmarOps takes the candidate's MSIX package for a Microsoft
    Store update of the previous version, and so accepts it without asking
    (invariant 8, winpkg.Accepts): RosmarOps's own package identity, which
    every MSIX leg installs; a package version equal to the binary's, which
    the test package has only when the version has no prerelease part (a
    pull request's candidate is X.Y.Z-journeys.N, its package X.Y.Z.0); and a
    version greater than the state's engine_version."""
    cand, pre = semver_core(version)
    prev, _ = semver_core(prev_version or "")
    return bool(msix) and cand is not None and pre == "" and prev is not None and cand > prev


def r2(args):
    j = Journey(args, "R2 MSIX" if args.msix_cand else "R2")
    lang = "es" if LOCALE_LANG else "en"
    yes = "s" if lang == "es" else "y"
    home = j.make_home("r2")
    if args.msix_prev:
        binary = install_msix(args.msix_prev)
        j.installed.append(binary)
    else:
        binary = j.install(home, args.prev)
    # The previous version, installed and used: state, history and config.
    prev_ok = (j.plain([binary, "init", "--repos-under", os.path.join(home, "projects")], home, lang, "r2-prev-init") == 0
               and j.plain([binary, "run", "--lang", lang], home, lang, "r2-prev-run") in (0, 1)
               and j.plain([binary, "run", "--unattended"], home, lang, "r2-prev-unattended") in (0, 1))
    if not j.check("the previous version installed and ran", prev_ok):
        return j.finish()
    if args.msix_cand:
        install_msix(args.msix_cand)  # the update, as the package installs it, same alias
    else:
        j.install(home, args.bin)  # the candidate, in the same path
    # Each command starts from this copy: the update done, nothing run yet.
    base = home + ".base"
    shutil.copytree(home, base, symlinks=True)
    state = os.path.join(".rosmarops", "state.json")
    with open(os.path.join(home, state), encoding="utf-8") as f:
        prev_version = json.load(f).get("engine_version", "")
    store = store_update(args.msix_cand, args.version, prev_version)
    print(f"  R2: {prev_version} → {args.version}, " + ("a Store update: no command asks" if store else "every command asks"))
    with open(os.path.join(CATALOG, lang + ".json"), encoding="utf-8") as f:
        # The fixed start of the update question, to check it never shows.
        changed = re.split(r"\{[a-z_]+\}", json.load(f).get("console.binary_changed", ""))[0].strip()
    for label, argv, kind in R2_ROWS:
        if store and kind in ("ask", "abort"):
            kind = "store"
        remove_tree(home)
        shutil.copytree(base, home, symlinks=True)
        before = os.path.join(j.work, "state-before.json")
        shutil.copyfile(os.path.join(home, state), before)
        slug = "r2-" + label.replace(" ", "-").replace("--", "")
        if kind == "store":
            windows = r2_windows(args.version).get(label, ())
            if "--unattended" in argv:
                code = j.plain([binary] + argv, home, lang, slug)
                ok, why = code == 0 and not red_report(home), f"exit {code}" + (", a red report" if red_report(home) else "")
            else:
                ok, why = j.drive("r2-store.steps", lang, [binary] + argv, home, slug, windows=windows)
                j.window_checks(home, os.path.join(j.out, slug + ".txt"), label, windows)
            said = open(os.path.join(j.out, slug + ".txt"), encoding="utf-8", errors="replace").read()
            if ok and changed and changed in said:
                ok, why = False, "the console shows the update question"
            j.check(f"{label}: a Store update, finishes with 0 without asking", ok, why)
            via = ""
            if os.path.exists(os.path.join(home, state)):
                with open(os.path.join(home, state), encoding="utf-8") as f:
                    via = json.load(f).get("integrity", {}).get("binary_accepted_via", "")
            j.check(f"{label}: the state records the update as accepted by the Store", via == "store",
                    f"binary_accepted_via {via!r}")
        elif kind == "ask":
            windows = r2_windows(args.version).get(label, ())
            ok, why = j.drive("r2-ask.steps", lang, [binary] + argv, home, slug, yes=yes, windows=windows)
            j.check(f"{label}: asks in the person's language and goes on", ok, why)
            j.window_checks(home, os.path.join(j.out, slug + ".txt"), label, windows)
        elif kind == "abort":
            code = j.plain([binary] + argv, home, lang, slug)
            red = red_report(home)
            j.check(f"{label}: stops with 2 and a red binary_hash_changed report", code == 2 and red,
                    f"exit {code}" + ("" if red else ", no red report"))
            console = os.path.join(j.out, slug + ".txt")
            c, why = j.run_check("text", "-catalog", CATALOG, "-lang", lang, console)
            # The stop's own text is the catalog's, in the person's language:
            # check b only sees catalog texts, not English written in the code.
            with open(os.path.join(CATALOG, lang + ".json"), encoding="utf-8") as f:
                stop = json.load(f).get("integrity.abort.binary.why", "")
            said = open(console, encoding="utf-8", errors="replace").read()
            ok = c == 0 and stop != "" and stop in said
            j.check(f"{label}: its console is the catalog's" + (", in Spanish" if lang == "es" else ""), ok,
                    why or ("" if stop else "the catalog has no integrity.abort.binary.why") or "the catalog's stop text is not in the console")
        else:
            ok, why = j.drive("r2-exit0.steps", lang, [binary] + argv, home, slug)
            text = open(os.path.join(j.out, slug + ".txt"), encoding="utf-8", errors="replace").read()
            if label == "--version":
                ok = ok and text.strip().endswith(f"rosmarops version {args.version}")
                why = why or text.strip()[-80:]
            elif label == "baseline":
                # No text sends the person to baseline to accept a version,
                # and it does not stop.
                ok = ok and "ABORT" not in text
            j.check(f"{label}: finishes with 0 without asking", ok, why)
        if os.path.exists(os.path.join(home, state)):
            c, why = j.run_check("salt", before, os.path.join(home, state))
            j.check(f"{label}: the same local_salt (the same computer in the space)", c == 0, why)
        if label == "no arguments":
            rep = j.review_checks(home, lang, os.path.join(j.out, slug + ".txt"), "r2 " + label)
            if rep:
                j.keep_report(rep, "after-update", lang, os.path.join(j.out, slug + ".txt"))
            if not store:  # a Store update asks no question to picture
                j.captures.append({"kind": "question", "path": os.path.join(j.out, slug + ".txt"), "step": "r2-question",
                                   "lang": lang, "until": "console.binary_confirm"})
    return j.finish()


def r3(args):
    j = Journey(args, "R3")
    lang = "es" if LOCALE_LANG else "en"
    for attempt in (1, 2):
        home = j.make_home(f"r3-{attempt}", medium=args.medium)
        binary = j.install(home, args.bin)
        code = j.plain([binary, "init", "--repos-under", os.path.join(home, "projects")], home, lang, f"r3-{attempt}-init")
        if not j.check(f"attempt {attempt}: init", code == 0, f"exit {code}"):
            return j.finish()
        reports = []
        for n in (1, 2):
            label = f"r3-{attempt}-run{n}"
            ok, why = j.drive("r3-run.steps", lang, [binary, "run"], home, label)
            j.check(f"attempt {attempt}, run {n}: ends without asking about each finding", ok, why)
            rep = j.reports_dir(home)
            if not rep:
                j.check(f"attempt {attempt}, run {n}: report written", False)
                return j.finish()
            keep = os.path.join(j.work, f"{label}.report.v2.json")
            shutil.copyfile(os.path.join(rep, "data", "report.v2.json"), keep)
            reports.append(keep)
        code, why = j.run_check("same", reports[0], reports[1])
        if code == 3 and attempt == 1:
            j.check("attempt 1: no repository ran out of time", False, "the machine was too slow: repeating once")
            j.results[-1]["status"] = "retried"
            continue
        if code == 3:
            j.check("no repository ran out of time", False, "the machine was too slow twice: not a valid comparison")
            break
        j.check(f"two runs in a row: the same findings, rules and score ({args.medium + 2} projects)", code == 0, why)
        break
    return j.finish()


def main():
    # The Windows console's code page cannot print the check marks.
    for stream in (sys.stdout, sys.stderr):
        stream.reconfigure(encoding="utf-8", errors="replace")
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("journey", choices=["r1", "r2", "r3"])
    ap.add_argument("--bin", required=True, help="the candidate binary")
    ap.add_argument("--prev", help="R2: the previous release's binary")
    ap.add_argument("--version", required=True, help="the candidate's version, without v")
    ap.add_argument("--tools", required=True, help="where makehome, check (and conpty) are built")
    ap.add_argument("--out", required=True)
    ap.add_argument("--medium", type=int, default=58, help="R3: generated repositories besides the two samples")
    ap.add_argument("--msix-cand", help="Windows: the folder with the candidate's MSIX test package and .cer")
    ap.add_argument("--msix-prev", help="Windows, R2: the folder with the previous version's MSIX test package")
    args = ap.parse_args()
    args.bin = os.path.abspath(args.bin)
    for k in ("prev", "msix_cand", "msix_prev"):
        if getattr(args, k):
            setattr(args, k, os.path.abspath(getattr(args, k)))
    sys.exit({"r1": r1, "r2": r2, "r3": r3}[args.journey](args))


if __name__ == "__main__":
    main()
