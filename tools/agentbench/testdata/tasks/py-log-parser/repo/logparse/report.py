"""Totals per job."""

from .durations import format_duration


def totals(records):
    out = {}
    for r in records:
        if r.status != "ok":
            continue
        out[r.job] = out.get(r.job, 0) + r.seconds
    return out


def summarize(records):
    lines = []
    for job, seconds in sorted(totals(records).items()):
        lines.append(f"{job:<12} {format_duration(seconds)}")
    return "\n".join(lines)
