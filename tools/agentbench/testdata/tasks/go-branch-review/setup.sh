#!/bin/sh
# Builds the feature branch the agent reviews: two commits on
# feature/quotas from branch/step1 and branch/step2, with fixed dates.
set -e
g() { git -c user.name="Dana Whitfield" -c user.email=dana@example.com "$@"; }
g checkout -q -b feature/quotas
cp -R "$TASK_DIR/branch/step1/." .
git add -A
GIT_AUTHOR_DATE=2026-09-21T09:12:00Z GIT_COMMITTER_DATE=2026-09-21T09:12:00Z g commit -q -m "quota: per-tenant request quota with a counter store"
cp -R "$TASK_DIR/branch/step2/." .
git add -A
GIT_AUTHOR_DATE=2026-09-21T14:40:00Z GIT_COMMITTER_DATE=2026-09-21T14:40:00Z g commit -q -m "server: enforce quotas on /v1/compute and report remaining"
