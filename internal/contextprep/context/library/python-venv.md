---
id: python-venv
description: Python, the project's virtualenv and pip's cache
files: [pyproject.toml, requirements.txt, setup.py]
check: [python3, --version]
enabled: false
---
Python: use the project's virtualenv (.venv/bin/python, or the one uv or poetry manages) instead of the system Python, and never pip install into the system. Point pip's cache at $TMPDIR when it cannot be written: PIP_CACHE_DIR=$TMPDIR/pip.
