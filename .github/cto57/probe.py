"""CTO-57 probe: read-only look at a clean Grok Bot install. Never sign in.

Prints names, sizes, key names and short code extracts only. Any long
token-like run of characters is redacted before printing.
"""
import json, os, re, sys, hashlib

HOME = os.environ["USERPROFILE"]
ROOTS = [os.environ["APPDATA"], os.environ["LOCALAPPDATA"], HOME]
TOKENISH = re.compile(rb"[A-Za-z0-9_\-+/=.]{24,}")
NEEDLES = [b"Execution on this computer", b"Execution on Local Computer", b"Ask every time",
           b"Always allow", b"Never allow", b"localExec", b"local_exec", b"localExecution",
           b"executionPolicy", b"execution_policy", b"executeOnThisComputer", b"localComputer"]
SESSIONISH = re.compile(r"(cookie|token|session|credential|auth|secret|keychain|login|local storage|leveldb|indexeddb)", re.I)


def red(b):
    return TOKENISH.sub(lambda m: m.group(0)[:4] + b"..." + m.group(0)[-4:], b)


def snapshot(path):
    out = {}
    for root in ROOTS:
        for dp, dn, fn in os.walk(root):
            depth = dp[len(root):].count(os.sep)
            if depth > 6:
                dn[:] = []
            for f in fn:
                p = os.path.join(dp, f)
                try:
                    st = os.stat(p)
                    out[p] = (st.st_size, st.st_mtime)
                except OSError:
                    pass
    json.dump(out, open(path, "w"))


def diff(before, after, label):
    a = json.load(open(before)); b = json.load(open(after))
    new = sorted(p for p in b if p not in a)
    changed = sorted(p for p in b if p in a and b[p] != a[p])
    print(f"== {label}: {len(new)} new, {len(changed)} changed files")
    for p in new + changed:
        tag = "NEW" if p in new else "CHG"
        if any(s in p.lower() for s in ("\\temp\\", "\\microsoft\\windows\\", "\\pip\\", "\\npm-cache\\", "\\nuget\\")):
            continue
        flag = " [SESSION-LIKE PATH]" if SESSIONISH.search(p) else ""
        print(f"{tag} {b[p][0]:>10} {p}{flag}")
    return new + changed


def keys_of(obj, prefix=""):
    if isinstance(obj, dict):
        for k, v in obj.items():
            yield prefix + k
            yield from keys_of(v, prefix + k + ".")
    elif isinstance(obj, list):
        for v in obj[:3]:
            yield from keys_of(v, prefix + "[].")


def inspect(files):
    print("== inspect small files (key names only; values only for execution-like keys)")
    for p in files:
        try:
            size = os.path.getsize(p)
        except OSError:
            continue
        if size > 2 * 1024 * 1024:
            continue
        try:
            data = open(p, "rb").read()
        except OSError as e:
            print(f"  unreadable {p}: {e}")
            continue
        hits = [n.decode() for n in NEEDLES if n.lower() in data.lower()]
        try:
            obj = json.loads(data.decode("utf-8"))
            ks = sorted(set(keys_of(obj)))
            print(f"-- JSON {p} ({size} B) keys={ks[:80]}")
            for k in ks:
                if re.search(r"exec|local|computer|approval|permission", k, re.I):
                    v = obj
                    try:
                        for part in k.split("."):
                            v = v[part]
                        if isinstance(v, (str, int, float, bool)) or v is None:
                            print(f"   {k} = {red(str(v).encode()).decode()}")
                    except Exception:
                        pass
        except Exception:
            if hits:
                print(f"-- BIN {p} ({size} B) needles={hits}")
        if hits:
            low = data.lower()
            for n in NEEDLES:
                i = low.find(n.lower())
                if i >= 0:
                    ctx = red(data[max(0, i - 160): i + 200]).decode("latin-1")
                    ctx = re.sub(r"[^\x20-\x7e]", ".", ctx)
                    print(f"   [{n.decode()}] @{i}: {ctx}")


def bundle(install_dir):
    print(f"== bundle search under {install_dir}")
    for dp, dn, fn in os.walk(install_dir):
        for f in fn:
            p = os.path.join(dp, f)
            try:
                size = os.path.getsize(p)
            except OSError:
                continue
            if size > 400 * 1024 * 1024 or not re.search(r"\.(js|cjs|mjs|json|asar|exe|dll|html|node)$", f, re.I):
                continue
            data = open(p, "rb").read()
            low = data.lower()
            for n in NEEDLES[:5]:
                start, count = 0, 0
                while count < 6:
                    i = low.find(n.lower(), start)
                    if i < 0:
                        break
                    ctx = red(data[max(0, i - 700): i + 900]).decode("latin-1")
                    ctx = re.sub(r"[^\x20-\x7e]", ".", ctx)
                    print(f"-- {p} ({size} B, sha256 {hashlib.sha256(data).hexdigest()[:16]}) [{n.decode()}] @{i}:\n{ctx}\n")
                    start, count = i + 1, count + 1


if __name__ == "__main__":
    cmd = sys.argv[1]
    if cmd == "snap":
        snapshot(sys.argv[2])
    elif cmd == "diff":
        diff(sys.argv[2], sys.argv[3], sys.argv[4])
    elif cmd == "inspect":
        inspect(diff(sys.argv[2], sys.argv[3], sys.argv[4]))
    elif cmd == "bundle":
        bundle(sys.argv[2])
