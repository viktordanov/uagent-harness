# Builds the two repositories the task's changelog covers, with fixed
# authors and dates so every run sees the same history.
set -e
n=0
commit() { # repo, subject, file
  n=$((n + 1))
  d="2026-0$(( (n / 10) + 6 ))-$(printf %02d $(( (n % 10) * 2 + 1 ))) 10:00:00 +0000"
  echo "$2" >> "$1/$3"
  git -C "$1" add -A
  GIT_AUTHOR_NAME="Dana Lee" GIT_AUTHOR_EMAIL=dana@example.com GIT_COMMITTER_NAME="Dana Lee" GIT_COMMITTER_EMAIL=dana@example.com \
    GIT_AUTHOR_DATE="$d" GIT_COMMITTER_DATE="$d" git -C "$1" commit -q -m "$2"
}
tag() { GIT_COMMITTER_DATE="2026-06-30 12:00:00 +0000" git -C "$1" tag -a "$2" -m "$2"; }
for r in app lib; do mkdir -p "$r" && git -C "$r" init -q -b main; done

commit lib "feat: add retry helper" retry.go
commit lib "fix: handle empty config file" config.go
tag lib v1.2.0
commit lib "feat(cache): add TTL-based eviction" cache.go
commit lib "chore: bump CI image to ubuntu-24.04" ci.yml
commit lib "fix(cache): avoid double close on shutdown" cache.go
commit lib "docs: describe cache options" README.md
commit lib "feat(log): add structured fields to errors" log.go
tag lib v1.3.0
commit lib "feat: experimental sharding" shard.go

commit app "feat: initial dashboard" dash.go
commit app "fix: login redirect loop" auth.go
tag app v1.2.0
commit app "feat(export): CSV export for reports" export.go
commit app "fix(auth): session cookie expired a day early" auth.go
commit app "chore(deps): update lockfile" go.sum
commit app "refactor: split report handler" report.go
commit app "fix(ui): misaligned table header in Safari" ui.css
tag app v1.3.0
commit app "fix: crash when exporting an empty report" export.go
