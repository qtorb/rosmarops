"""CTO-57: find the Windows installer link behind x.ai/bot (public pages only)."""
import os, re, urllib.request, urllib.parse

UA = {"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"}
LINK = re.compile(r"https://(?:downloads\.cursor\.com/grokbot|api2\.cursor\.sh/updates/download)/[^\"'\s<>\\`)]+")
TPL = re.compile(r"(\w+):`\$\{\w+\}/([a-z0-9-]+)/(grok-bot-[0-9a-f]+)`")
ANY = re.compile(r"[\"'`]([^\"'`\s]*(?:grokbot|grok-bot|Grok_Bot|win32-x64|downloads\.cursor)[^\"'`\s]*)[\"'`]", re.I)


def get(url):
    req = urllib.request.Request(url, headers=UA)
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.status, r.geturl(), r.read().decode("utf-8", "replace")


found, seen = set(), set()
pages = ["https://x.ai/bot", "https://docs.x.ai/grok-bot/get-started"]
for page in pages:
    try:
        st, final, html = get(page)
    except Exception as e:
        print("page", page, "failed:", e); continue
    print("page", page, "->", st, final, len(html))
    for m in LINK.findall(html): found.add(m)
    for m in ANY.findall(html): print("  hint:", m[:200])
    for m in re.finditer(r"(?i)download", html):
        ctx = re.sub(r"\s+", " ", html[max(0, m.start() - 150): m.start() + 200])
        if re.search(r"(?i)win|exe|href|url", ctx): print("  ctx:", ctx[:350])
    scripts = re.findall(r"<script[^>]+src=[\"']([^\"']+)[\"']", html)
    scripts += re.findall(r"[\"'](/_next/static/[^\"']+\.js)[\"']", html)
    for s in scripts:
        u = urllib.parse.urljoin(final, s)
        if u in seen or len(seen) > 120: continue
        seen.add(u)
        try:
            _, _, js = get(u)
        except Exception as e:
            continue
        for m in LINK.findall(js): found.add(m)
        for k, plat, slug in TPL.findall(js):
            print('  template', k, plat, slug)
            found.add(f'https://api2.cursor.sh/updates/download/stable/{plat}/{slug}')
        for m in ANY.findall(js): print("  js hint", u.rsplit("/", 1)[-1], ":", m[:200])
        for m in re.finditer(r"(?i)win32-x64|Grok_Bot_|grokbot/stable|downloads\.cursor\.com", js):
            print("  js ctx", u.rsplit("/", 1)[-1], ":", re.sub(r"\s+", " ", js[max(0, m.start() - 300): m.start() + 300]))
print("scripts fetched:", len(seen))
print("links:", sorted(found))
pick = next((l for l in sorted(found) if re.search(r"/win32-x64(-user)?/", l)), None)
if not pick:
    pick = next((l for l in sorted(found) if "win32-x64" in l), None)
if not pick:
    slug = next((re.search(r"(grok-bot-[0-9a-f]+)", l).group(1) for l in found if "grok-bot-" in l), None)
    for plat in ("win32-x64-user", "win32-x64", "win32-x64-system"):
        if not slug: break
        u = f"https://api2.cursor.sh/updates/download/stable/{plat}/{slug}"
        try:
            req = urllib.request.Request(u, headers=UA, method="HEAD")
            with urllib.request.urlopen(req, timeout=30) as r:
                print("HEAD", u, r.status, r.geturl()); pick = u; break
        except Exception as e:
            print("HEAD", u, "->", e)
if not pick:
    raise SystemExit("no Windows installer link found")
print("picked", pick)
open(os.environ["GITHUB_ENV"], "a").write(f"GB_URL={pick}\n")
