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
import shutil
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
        with open(os.path.join(self.out, "results.json"), "w", encoding="utf-8") as f:
            json.dump({"journey": self.name, "system": SYSTEM, "version": self.args.version,
                       "passed": not self.failed(), "results": self.results, "captures": self.captures}, f, indent=1)
        return 1 if self.failed() else 0

    # --- the test computer --------------------------------------------------
    def make_home(self, label, medium=0):
        home = os.path.join(self.work, label)
        manifest = os.path.join(self.work, label + "-manifest.json")
        subprocess.run([self.tool("makehome"), "-home", home, "-manifest", manifest, "-fixture",
                        os.path.join(ROOT, "testdata", "sample-home"), "-medium", str(medium), "-large", "0"],
                       check=True, stdout=subprocess.DEVNULL)
        return home

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
    def drive(self, steps, lang, argv, home, label, yes=None):
        """Run argv in a terminal through the steps; the console is kept as
        label.txt. Returns (ok, why)."""
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
        p = subprocess.run(cmd, env=self.env(home, lang), cwd=home, capture_output=True, text=True,
                           encoding="utf-8", errors="replace")
        why = (p.stderr or p.stdout).strip().splitlines()
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
        code, why = self.run_check("leaks", "-home", home, "-forbid", ",".join(forbid), console,
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
        args.bin = install_msix(args.msix_cand)
    legs = ["es", "en"] if LOCALE_LANG else ["en"]
    for lang in legs:
        label = f"r1-{lang}"
        home = j.make_home(label)
        ok, why = j.drive("r1.steps", lang, [args.bin], home, label)
        j.check(f"{label}: no arguments ends without an unplanned question", ok, why)
        rep = j.review_checks(home, lang, os.path.join(j.out, label + ".txt"), label)
        if SYSTEM == "linux":
            j.snap_gate(label)
        if rep:
            j.keep_report(rep, "first-run", lang, os.path.join(j.out, label + ".txt"))
        if not LOCALE_LANG:
            # The Spanish leg: on these systems the language does not follow
            # the locale, so it is asked for.
            label = "r1-es"
            ok, why = j.drive("r1-run.steps", "es", [args.bin, "run", "--lang", "es"], home, label)
            j.check(f"{label}: run --lang es ends without an unplanned question", ok, why)
            rep = j.review_checks(home, "es", os.path.join(j.out, label + ".txt"), label)
            if rep:
                j.keep_report(rep, "run-lang-es", "es", os.path.join(j.out, label + ".txt"))
    return j.finish()


# R2's commands after the update: (label, argv, kind, how to judge it).
#   ask:   asks whether the person installed this version, takes the answer,
#          finishes with 0;
#   abort: without a terminal, stops with 2 and a red report;
#   exit0: finishes with 0 without asking.
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


def r2(args):
    j = Journey(args, "R2 MSIX" if args.msix_cand else "R2")
    lang = "es" if LOCALE_LANG else "en"
    yes = "s" if lang == "es" else "y"
    home = j.make_home("r2")
    if args.msix_prev:
        binary = install_msix(args.msix_prev)
    else:
        binary = os.path.join(j.work, "bin", "rosmarops" + EXE)
        os.makedirs(os.path.dirname(binary))
        shutil.copyfile(args.prev, binary)
        os.chmod(binary, 0o755)
    # The previous version, installed and used: state, history and config.
    prev_ok = (j.plain([binary, "init", "--repos-under", os.path.join(home, "projects")], home, lang, "r2-prev-init") == 0
               and j.plain([binary, "run", "--lang", lang], home, lang, "r2-prev-run") in (0, 1)
               and j.plain([binary, "run", "--unattended"], home, lang, "r2-prev-unattended") in (0, 1))
    if not j.check("the previous version installed and ran", prev_ok):
        return j.finish()
    base = home + ".base"
    shutil.copytree(home, base, symlinks=True)
    if args.msix_cand:
        args.bin = install_msix(args.msix_cand)  # the update, as the package installs it
    else:
        shutil.copyfile(args.bin, binary)  # the candidate, in the same path
        os.chmod(binary, 0o755)
    state = os.path.join(".rosmarops", "state.json")
    for label, argv, kind in R2_ROWS:
        shutil.rmtree(home)
        shutil.copytree(base, home, symlinks=True)
        before = os.path.join(j.work, "state-before.json")
        shutil.copyfile(os.path.join(home, state), before)
        slug = "r2-" + label.replace(" ", "-").replace("--", "")
        if kind == "ask":
            ok, why = j.drive("r2-ask.steps", lang, [binary] + argv, home, slug, yes=yes)
            j.check(f"{label}: asks in the person's language and goes on", ok, why)
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
            j.captures.append({"kind": "question", "path": os.path.join(j.out, slug + ".txt"), "step": "r2-question",
                               "lang": lang, "until": "console.binary_confirm"})
    return j.finish()


def r3(args):
    j = Journey(args, "R3")
    lang = "es" if LOCALE_LANG else "en"
    for attempt in (1, 2):
        home = j.make_home(f"r3-{attempt}", medium=args.medium)
        code = j.plain([args.bin, "init", "--repos-under", os.path.join(home, "projects")], home, lang, f"r3-{attempt}-init")
        if not j.check(f"attempt {attempt}: init", code == 0, f"exit {code}"):
            return j.finish()
        reports = []
        for n in (1, 2):
            label = f"r3-{attempt}-run{n}"
            ok, why = j.drive("r3-run.steps", lang, [args.bin, "run"], home, label)
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
