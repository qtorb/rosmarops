#!/usr/bin/env python3
"""drive: run a command in a pseudo-terminal and walk it through a journey.

The journey tests (journeys.yml) use it on Linux and macOS; on Windows the
same steps are run by the Go driver over ConPTY (tests/journeys/drive/conpty).
Only the Python standard library: it is a test tool, not part of RosmarOps.

A steps file has one step per line ('#' starts a comment):

    expect      <catalog key or text>   [timeout <N>s]
    send        <text>                  (\\r, \\n and \\t are escapes)
    expect-exit <code>                  [timeout <N>s]
    if-asked    <catalog key or text> send <text>

if-asked is a question that may come or not (a system shows it, another does
not): from that line on, each time it appears it is answered with the text.
Any other question still fails the journey by the idle rule below.

An expect that names a key of the catalog (internal/catalog/messages/<lang>.json
of the same commit) waits for that text in the language given, its {params}
matching anything; any other expect waits for the text as written. The output
is compared without colours or cursor codes and with runs of spaces and line
breaks as one space, so a wrapped line still matches.

Each expect has its time limit (default 120 s); expect-exit too (default
600 s). While waiting for the exit, a process that has written nothing for
--idle seconds (default 30) fails with «waiting for an answer the journey did
not expect»: that is how a question nobody planned for is caught. Any failure
prints the last screen and exits 1. The whole output is kept in --log (as
written) and in --text (plain).
"""

import argparse
import json
import os
import pty
import re
import select
import signal
import struct
import sys
import time

try:
    import fcntl
    import termios
except ImportError:  # pragma: no cover - not on Windows
    fcntl = termios = None

ANSI = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-Z\\-_]")
PARAM = re.compile(r"\{[a-z_]+\}")
STEP = re.compile(r"^(expect-exit|expect|send)\s+(.*?)(?:\s+timeout\s+(\d+)s)?\s*$")
IFASKED = re.compile(r"^if-asked\s+(.*?)\s+send\s+(.*)$")


def plain(raw):
    """The output without escape codes or carriage returns."""
    text = ANSI.sub("", raw)
    return text.replace("\r\n", "\n").replace("\r", "\n")


def squeeze(text):
    return re.sub(r"\s+", " ", text)


def load_catalog(directory, lang):
    with open(os.path.join(directory, lang + ".json"), encoding="utf-8") as f:
        return json.load(f)


def pattern_for(arg, catalog):
    """A regular expression for an expect argument: a catalog text with its
    parameters as wildcards, or the literal text."""
    text = catalog.get(arg, arg)
    parts = PARAM.split(squeeze(text).strip())
    return re.compile(".*?".join(re.escape(p) for p in parts), re.S)


def unescape(s):
    return s.replace("\\r", "\r").replace("\\n", "\n").replace("\\t", "\t")


def parse(path):
    steps = []
    with open(path, encoding="utf-8") as f:
        for n, line in enumerate(f, 1):
            line = line.rstrip("\n")
            if not line.strip() or line.lstrip().startswith("#"):
                continue
            a = IFASKED.match(line.strip())
            if a:
                steps.append(("if-asked", (a.group(1), a.group(2)), None, n))
                continue
            m = STEP.match(line.strip())
            if not m:
                raise SystemExit(f"{path}:{n}: not a step: {line!r}")
            steps.append((m.group(1), m.group(2), int(m.group(3)) if m.group(3) else None, n))
    return steps


class Session:
    def __init__(self, argv, env, cols, rows):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:  # child
            try:
                os.execvpe(argv[0], argv, env)
            finally:
                os._exit(127)
        if termios:
            fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.raw = ""
        self.last_output = time.monotonic()
        self.status = None
        self.handlers = []  # [regex, answer, where it last answered]

    def answer_asked(self):
        """Answer each if-asked question that appeared since it last did."""
        text = squeeze(plain(self.raw))
        for h in self.handlers:
            m = h[0].search(text, h[2])
            if m:
                h[2] = m.end()
                os.write(self.fd, h[1].encode("utf-8"))

    def pump(self, wait):
        """Read what is there within wait seconds; False once the output is
        closed."""
        r, _, _ = select.select([self.fd], [], [], wait)
        if not r:
            return True
        try:
            data = os.read(self.fd, 65536)
        except OSError:
            return False
        if not data:
            return False
        self.raw += data.decode("utf-8", "replace")
        self.last_output = time.monotonic()
        self.answer_asked()
        return True

    def exited(self):
        if self.status is None:
            pid, st = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                self.status = os.waitstatus_to_exitcode(st)
        return self.status is not None

    def kill(self):
        if not self.exited():
            os.kill(self.pid, signal.SIGKILL)
            os.waitpid(self.pid, 0)


def fail(session, why, args):
    session.kill()
    save(session, args)
    screen = "\n".join(plain(session.raw).splitlines()[-40:])
    print(f"drive: FAIL: {why}\n--- last screen ---\n{screen}\n---", file=sys.stderr)
    sys.exit(1)


def save(session, args):
    if args.log:
        with open(args.log, "w", encoding="utf-8") as f:
            f.write(session.raw)
    if args.text:
        with open(args.text, "w", encoding="utf-8") as f:
            f.write(plain(session.raw))


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--steps", required=True)
    ap.add_argument("--catalog", required=True, help="internal/catalog/messages")
    ap.add_argument("--lang", default="en")
    ap.add_argument("--idle", type=int, default=30)
    ap.add_argument("--cols", type=int, default=200)
    ap.add_argument("--rows", type=int, default=50)
    ap.add_argument("--log")
    ap.add_argument("--text")
    ap.add_argument("command", nargs=argparse.REMAINDER)
    args = ap.parse_args()
    argv = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not argv:
        ap.error("no command")
    catalog = load_catalog(args.catalog, args.lang)
    steps = parse(args.steps)
    s = Session(argv, dict(os.environ), args.cols, args.rows)
    seen = 0  # where the next expect starts looking
    for kind, arg, limit, line in steps:
        if kind == "if-asked":
            s.handlers.append([pattern_for(arg[0], catalog), unescape(arg[1]), seen])
            continue
        if kind == "send":
            os.write(s.fd, unescape(arg).encode("utf-8"))
            continue
        if kind == "expect":
            rx = pattern_for(arg, catalog)
            deadline = time.monotonic() + (limit or 120)
            while True:
                text = squeeze(plain(s.raw))
                m = rx.search(text, seen)
                if m:
                    seen = m.end()
                    break
                if time.monotonic() > deadline:
                    fail(s, f"line {line}: no {arg!r} within {limit or 120} s", args)
                if not s.pump(0.2) and s.exited():
                    text = squeeze(plain(s.raw))
                    if rx.search(text, seen):
                        continue
                    fail(s, f"line {line}: exited ({s.status}) before {arg!r}", args)
            continue
        # expect-exit
        want = int(arg)
        deadline = time.monotonic() + (limit or 600)
        while not s.exited():
            open_ = s.pump(0.2)
            if s.exited():
                break
            if time.monotonic() > deadline:
                fail(s, f"line {line}: still running after {limit or 600} s", args)
            if time.monotonic() - s.last_output > args.idle:
                fail(s, f"line {line}: waiting for an answer the journey did not expect "
                        f"(no output for {args.idle} s)", args)
            if not open_:
                time.sleep(0.1)
        while s.pump(0.1):
            pass
        if s.status != want:
            fail(s, f"line {line}: exit code {s.status}, want {want}", args)
    s.kill()
    save(s, args)
    print(f"drive: OK ({len(steps)} steps, exit {s.status})")


if __name__ == "__main__":
    main()
