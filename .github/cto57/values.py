"""CTO-57: values of ~/.grokbot small JSON files (booleans, numbers, short strings; long strings redacted)."""
import json, os, re
d = os.path.join(os.environ["USERPROFILE"], ".grokbot")
def show(v):
    if isinstance(v, str):
        return repr(v[:4] + "..." + v[-4:]) if len(v) > 24 else repr(v)
    if isinstance(v, (dict, list)):
        return json.dumps(v)[:200] if len(json.dumps(v)) < 200 and not re.search(r"[A-Za-z0-9_\-]{24,}", json.dumps(v)) else f"<{type(v).__name__} len {len(v)}>"
    return repr(v)
for dp, dn, fn in os.walk(d):
    for f in fn:
        p = os.path.join(dp, f); s = os.path.getsize(p)
        print(f"-- {p} ({s} B)")
        if s > 65536: continue
        try:
            o = json.load(open(p, encoding="utf-8"))
        except Exception as e:
            print("   not JSON"); continue
        if isinstance(o, dict):
            for k, v in o.items(): print(f"   {k} = {show(v)}")
