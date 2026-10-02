"""The agentbench check of ci-log-triage: ci/triage.csv against the
generator's truth, and ci/TRIAGE.md naming each regression's commit."""
import csv
import os
import subprocess
import sys

truth = list(csv.DictReader(subprocess.run([sys.executable, os.path.join(os.environ["TASK_DIR"], "gen.py"), "--truth"], capture_output=True, text=True, check=True).stdout.splitlines()))
want = {r["job"]: r for r in truth}
got = {}
try:
    with open("ci/triage.csv", newline="") as f:
        for r in csv.DictReader(f):
            job = (r.get("job") or "").strip().removeprefix("job-").removesuffix(".log")
            got[job] = {k: (v or "").strip() for k, v in r.items() if k}
except FileNotFoundError:
    sys.exit("no ci/triage.csv")
problems = []
cause_ok = test_ok = test_n = commit_ok = commit_n = 0
for job, w in want.items():
    g = got.get(job)
    if g is None:
        problems.append(f"{job}: missing")
        continue
    if g.get("cause") == w["cause"]:
        cause_ok += 1
    else:
        problems.append(f"{job}: cause {g.get('cause')!r}, want {w['cause']}")
    if w["test"]:
        test_n += 1
        if g.get("test", "").split("/")[0] == w["test"]:
            test_ok += 1
        else:
            problems.append(f"{job}: test {g.get('test')!r}, want {w['test']}")
    if w["commit"]:
        commit_n += 1
        c = g.get("commit", "")
        if len(c) >= 7 and w["commit"].startswith(c[:7]):
            commit_ok += 1
        else:
            problems.append(f"{job}: commit {c!r}, want {w['commit']}")
print(f"causes {cause_ok}/{len(want)}, tests {test_ok}/{test_n}, commits {commit_ok}/{commit_n}")
for p in problems:
    print(" ", p)
ok = cause_ok >= len(want) - 3 and test_ok >= test_n - 2 and commit_ok >= commit_n - 1
try:
    md = open("ci/TRIAGE.md").read()
except FileNotFoundError:
    md = ""
    print("no ci/TRIAGE.md")
    ok = False
for r in {r["commit"]: r for r in truth if r["commit"]}.values():
    if r["commit"] not in md or r["test"] not in md:
        print(f"TRIAGE.md does not name {r['test']} with {r['commit']}")
        ok = False
sys.exit(0 if ok else 1)
