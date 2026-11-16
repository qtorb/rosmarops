"""CTO-57 pass 2: where the app stores local tool access. Extracted app code only, plus ~/.grokbot values."""
import json, os, re, sys

root = sys.argv[1]
TOK = re.compile(r"[A-Za-z0-9_\-+/=.]{32,}")
PATS = os.environ["CTO57_PATS"].split("|||") if os.environ.get("CTO57_PATS") else [r"localToolAccess\w*", r"local[-_]?tool[-_]?(?:access|scope|policy|mode|setting)\w*", r"localExecution\w*",
        r"executionOnLocal\w*", r"localExec\w*", r"local-computer-use-gate", r"computer-use-gate", r"-gate\.json",
        r"egressTunnelEnabled", r"settings\.json", r"\.grokbot", r"\"(?:ask|always|never)\"\s*[,:\]]"]
seen = {}
for dp, dn, fn in os.walk(root):
    dn[:] = [d for d in dn if d != "node_modules" or dp == root]
    for f in fn:
        if not f.endswith((".js", ".cjs", ".mjs", ".json")):
            continue
        p = os.path.join(dp, f)
        try:
            t = open(p, encoding="utf-8", errors="replace").read()
        except OSError:
            continue
        rel = os.path.relpath(p, root)
        for pat in PATS:
            for m in re.finditer(pat, t):
                key = (pat, m.group(0))
                n = seen.get(key, 0)
                if n >= int(os.environ.get('CTO57_MAX','3')):
                    continue
                seen[key] = n + 1
                W = int(os.environ.get('CTO57_W','700')); ctx = t[max(0, m.start() - W): m.end() + W]
                ctx = TOK.sub(lambda x: x.group(0)[:4] + "..." + x.group(0)[-4:], ctx)
                print(f"-- [{pat}] {m.group(0)} in {rel}@{m.start()}:\n{ctx}\n")
print("== distinct identifiers")
for (pat, s), n in sorted(seen.items()):
    print(f"  {pat} :: {s} x{n}")
