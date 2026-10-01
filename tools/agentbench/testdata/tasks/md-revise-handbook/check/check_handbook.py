"""Checks the revisions asked of docs/handbook.md."""
import json
import re
import sys

PATH = "docs/handbook.md"
problems = []


def need(ok, msg):
    if not ok:
        problems.append(msg)


doc = open(PATH, encoding="utf-8").read()
need(len(doc.encode()) >= 35 * 1024, f"handbook shrank to {len(doc.encode())} bytes")

# 1. The product is Control Center in prose; code blocks keep OpsPortal.
fences = re.findall(r"```.*?```", doc, flags=re.S)
prose = re.sub(r"```.*?```", "", doc, flags=re.S)
need("OpsPortal" not in prose, f"OpsPortal still in prose {prose.count('OpsPortal')} times")
need(prose.count("Control Center") >= 80, f"Control Center only {prose.count('Control Center')} times in prose")
for f in json.load(open("handbook_fences.json")):
    need(f in fences, "a code block was changed or removed: " + f.splitlines()[1][:60])

# 2. Headings: SOAP gone, Rotating credentials right after Managing API keys.
heads = re.findall(r"^## (.+)$", prose, flags=re.M)
want = ["Contents", "Control Center overview", "Getting access", "Managing API keys", "Rotating credentials",
        "Deployments", "Monitoring and alerts", "Incident escalation", "Support tiers", "Backups",
        "Data retention", "Integrations", "Troubleshooting", "Glossary", "FAQ"]
need(heads == want, f"sections are {heads}")
need("SOAP" not in doc, "SOAP is still mentioned")
m = re.search(r"^## Rotating credentials\n(.*?)(?=^## )", doc, flags=re.M | re.S)
need(m is not None and len(m.group(1).strip()) >= 200, "Rotating credentials section is missing or too short")

# The table of contents lists the sections.
toc = re.search(r"^## Contents\n(.*?)(?=^## )", doc, flags=re.M | re.S)
items = re.findall(r"^- \[([^\]]+)\]", toc.group(1), flags=re.M) if toc else []
need(items == want[1:], f"contents list {items}")

# 3. The escalation list is numbered 1..6 in its original order.
esc = re.search(r"^## Incident escalation\n(.*?)(?=^## )", doc, flags=re.M | re.S)
steps = re.findall(r"^(\d+)\. (.+)$", esc.group(1), flags=re.M) if esc else []
need([int(n) for n, _ in steps] == [1, 2, 3, 4, 5, 6], f"escalation numbers {[n for n, _ in steps]}")
need(len(steps) == 6 and steps[0][1].startswith("Acknowledge") and steps[5][1].startswith("Write the timeline"),
     "escalation steps changed")

# 4. Support tiers.
rows = dict(re.findall(r"^\| (Bronze|Silver|Gold|Platinum) \| ([^|]+?) \|", doc, flags=re.M))
need(rows == {"Bronze": "2 business days", "Silver": "1 business day", "Gold": "4 hours", "Platinum": "1 hour"},
     f"support tiers {rows}")

if problems:
    print("FAIL:\n- " + "\n- ".join(problems))
    sys.exit(1)
print("ok")
