"""Durations as the job runner writes them: "SS", "MM:SS", or "HH:MM:SS"."""


def parse_duration(text):
    """Return the number of seconds in a duration string."""
    parts = [int(p) for p in text.strip().split(":")]
    if not parts or len(parts) > 3:
        raise ValueError(f"bad duration: {text!r}")
    seconds = 0
    for p in parts[-2:]:
        seconds = seconds * 60 + p
    return seconds


def format_duration(seconds):
    """Format seconds as H:MM:SS."""
    h, rest = divmod(int(seconds), 3600)
    m, s = divmod(rest, 60)
    return f"{h}:{m:02d}:{s:02d}"
