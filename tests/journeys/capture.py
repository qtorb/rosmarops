#!/usr/bin/env python3
"""capture: the pictures of a journey for the website, and their manifest.

    capture.py --results OUT/results.json --dest DIR [--firefox CMD]

From what a journey (journey.py) kept, it draws with the runner's Firefox,
headless:
  - the console of each step, as plain text (no colours) and as a PNG 1280 px
    wide;
  - the report pages, in Spanish and English, as PNGs 1280 and 390 px wide;
  - in R2, the screen of the update question, apart.
It writes DIR/manifest.json: per picture, system, journey, step, language,
width, candidate version, UTC date, the run's link and status: ok when the
journey passed, fallido when not. A picture of a failed journey is never ok.
A picture Firefox cannot take within 60 s is left out and said in the log.

--firefox is the command that starts Firefox (on Linux, «snap run firefox»:
then DIR must be in a visible folder of the home, which a snap can read).
Only the Python standard library. A test tool, never part of RosmarOps.
"""

import argparse
import datetime
import html
import json
import os
import pathlib
import re
import shlex
import shutil
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ANSI = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-Z\\-_]")
PARAM = re.compile(r"\{[a-z_]+\}")


def console_page(text, title):
    return ("<!doctype html><html><head><meta charset='utf-8'><title>" + html.escape(title) + "</title>"
            "<style>body{margin:0;background:#101418;color:#e6e6e6}"
            "pre{margin:0;padding:24px;font:15px/1.45 ui-monospace,Menlo,Consolas,monospace;white-space:pre-wrap}"
            "</style></head><body><pre>" + html.escape(text) + "</pre></body></html>")


def screenshot(firefox, page, png, width):
    if os.path.exists(png):
        os.remove(png)
    cmd = shlex.split(firefox) + ["--headless", "--screenshot", png, f"--window-size={width},900", pathlib.Path(page).as_uri()]
    try:
        subprocess.run(cmd, timeout=60, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except subprocess.TimeoutExpired:
        subprocess.run(["pkill", "-f", "firefox.*--headless"], stderr=subprocess.DEVNULL) if shutil.which("pkill") else None
    return os.path.exists(png) and os.path.getsize(png) > 0


def until(text, key, lang):
    """The console up to the end of the catalog text key (the question)."""
    with open(os.path.join(ROOT, "internal", "catalog", "messages", lang + ".json"), encoding="utf-8") as f:
        want = json.load(f).get(key, key)
    head = PARAM.split(want)[0].strip()
    i = text.find(head)
    if i < 0:
        return text
    return text[: i + len(want.split("{")[0].rstrip())]


def main():
    # The Windows console's code page cannot print the check marks.
    for stream in (sys.stdout, sys.stderr):
        stream.reconfigure(encoding="utf-8", errors="replace")
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--results", required=True)
    ap.add_argument("--dest", required=True)
    ap.add_argument("--firefox", default="firefox")
    args = ap.parse_args()
    with open(args.results, encoding="utf-8") as f:
        res = json.load(f)
    dest = os.path.abspath(args.dest)
    os.makedirs(dest, exist_ok=True)
    status = "ok" if res["passed"] else "fallido"
    run = ""
    if os.environ.get("GITHUB_RUN_ID"):
        run = f"{os.environ.get('GITHUB_SERVER_URL', 'https://github.com')}/{os.environ.get('GITHUB_REPOSITORY')}/actions/runs/{os.environ['GITHUB_RUN_ID']}"
    when = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    system, journey = res["system"], res["journey"].lower().replace(" ", "-")
    items = []

    def add(path, step, lang, width):
        items.append({"file": os.path.basename(path), "system": system, "journey": res["journey"], "step": step,
                      "lang": lang, "width": width, "version": res["version"], "date_utc": when, "run": run,
                      "status": status})

    for c in res["captures"]:
        base = f"{system}-{journey}-{c['step']}-{c['lang']}"
        if c["kind"] in ("console", "question"):
            with open(c["path"], encoding="utf-8", errors="replace") as f:
                text = ANSI.sub("", f.read()).replace("\r\n", "\n").replace("\r", "\n")
            if c["kind"] == "question":
                text = until(text, c["until"], c["lang"])
                base = f"{system}-{journey}-question-{c['lang']}"
            txt = os.path.join(dest, base + "-console.txt")
            with open(txt, "w", encoding="utf-8") as f:
                f.write(text)
            add(txt, c["step"], c["lang"], None)
            page = os.path.join(dest, base + "-console.html")
            with open(page, "w", encoding="utf-8") as f:
                f.write(console_page(text, base))
            png = os.path.join(dest, base + "-console-1280.png")
            if screenshot(args.firefox, page, png, 1280):
                add(png, c["step"], c["lang"], 1280)
            else:
                print(f"capture: no screenshot of {page} (1280)")
            os.remove(page)
        elif c["kind"] == "report":
            page = os.path.join(dest, base + "-report.html")
            shutil.copyfile(c["path"], page)
            for width in (1280, 390):
                png = os.path.join(dest, f"{base}-report-{width}.png")
                if screenshot(args.firefox, page, png, width):
                    add(png, c["step"], c["lang"], width)
                else:
                    print(f"capture: no screenshot of {page} ({width})")
            os.remove(page)
    with open(os.path.join(dest, f"manifest-{system}-{journey}.json"), "w", encoding="utf-8") as f:
        json.dump(items, f, indent=1, ensure_ascii=False)
    print(f"capture: {len(items)} pictures and texts, status {status}")


if __name__ == "__main__":
    main()
