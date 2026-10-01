"""One log line is: <date> <time> <job> <status> <duration>."""

from dataclasses import dataclass

from .durations import parse_duration


@dataclass
class Record:
    job: str
    status: str
    seconds: int


def parse_line(line):
    fields = line.split()
    if len(fields) != 5:
        return None
    _date, _time, job, status, duration = fields
    return Record(job=job, status=status.lower(), seconds=parse_duration(duration))


def parse_lines(lines):
    out = []
    for line in lines:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        rec = parse_line(line)
        if rec is not None:
            out.append(rec)
    return out
