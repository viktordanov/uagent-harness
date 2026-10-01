"""Checks that only tooly's notification hook went."""
import os
import sys
import tomllib

home = os.environ["HOME"]
orig = os.path.join(os.path.dirname(os.path.abspath(__file__)), "orig")
failures = []

path = os.path.join(home, ".config/tooly/config.toml")
try:
    got = tomllib.load(open(path, "rb"))
except (OSError, tomllib.TOMLDecodeError) as e:
    sys.exit(f"{path}: {e}")
want = tomllib.load(open(os.path.join(orig, "tooly.toml"), "rb"))
want["hooks"] = [h for h in want["hooks"] if h["name"] != "notify"]
if got != want:
    failures.append(f"{path}: want the same settings and hooks without notify, got hooks {[h.get('name') for h in got.get('hooks', [])]}")

for name, rel in (("toolyrc", ".toolyrc"), ("watchr.toml", ".config/watchr/config.toml"), ("zshrc", ".zshrc")):
    p = os.path.join(home, rel)
    if not os.path.exists(p) or open(p, "rb").read() != open(os.path.join(orig, name), "rb").read():
        failures.append(f"{p}: changed, but it is not tooly's active config")

for f in failures:
    print(f)
sys.exit(1 if failures else 0)
