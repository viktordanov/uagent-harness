"""The reference solution of ci-log-triage: the generator's truth, written
as the agent would."""
import csv
import io
import os
import subprocess
import sys

truth = subprocess.run([sys.executable, os.path.join(os.environ["TASK_DIR"], "gen.py"), "--truth"], capture_output=True, text=True, check=True).stdout
with open("ci/triage.csv", "w") as f:
    f.write(truth)
rows = list(csv.DictReader(io.StringIO(truth)))
md = ["# Triage of the failed nightly jobs", ""]
for cause in sorted({r["cause"] for r in rows}):
    md.append(f"- {cause}: {sum(r['cause'] == cause for r in rows)}")
md.append("")
for commit in sorted({r["commit"] for r in rows if r["commit"]}):
    rs = [r for r in rows if r["commit"] == commit]
    md.append(f"- {rs[0]['test']}, broken by {commit}, failed jobs {', '.join(r['job'] for r in rs)}")
with open("ci/TRIAGE.md", "w") as f:
    f.write("\n".join(md) + "\n")
