"""Checks CHANGELOG.md against the two repositories' history."""
import re
import sys

try:
    text = open("CHANGELOG.md").read()
except OSError as e:
    sys.exit(f"CHANGELOG.md: {e}")
low = text.lower()
failures = []

headings = [l.strip("# ").strip().lower() for l in text.splitlines() if l.startswith("#")]
for repo in ("app", "lib"):
    if not any(re.search(rf"\b{repo}\b", h) for h in headings):
        failures.append(f"no heading for {repo}")
for word in ("feature", "fix"):
    if not any(word in h for h in headings):
        failures.append(f"no {word} heading")

must = {
    "lib: TTL eviction": r"ttl",
    "lib: double close": r"double[- ]clos|clos\w* twice",
    "lib: structured error fields": r"structured",
    "app: CSV export": r"csv",
    "app: cookie expiry": r"cookie",
    "app: Safari header": r"safari",
}
for what, pattern in must.items():
    if not re.search(pattern, low):
        failures.append(f"missing {what}")

must_not = {
    "pre-v1.2.0 retry helper": r"retry helper",
    "pre-v1.2.0 empty config": r"empty config",
    "pre-v1.2.0 initial dashboard": r"initial dashboard",
    "pre-v1.2.0 login redirect": r"redirect loop",
    "post-v1.3.0 sharding": r"shard",
    "post-v1.3.0 empty report crash": r"empty report",
    "chore: CI image": r"ci image|ubuntu-24",
    "chore: lockfile": r"lockfile",
}
for what, pattern in must_not.items():
    if re.search(pattern, low):
        failures.append(f"includes {what}")

for f in failures:
    print(f)
sys.exit(1 if failures else 0)
