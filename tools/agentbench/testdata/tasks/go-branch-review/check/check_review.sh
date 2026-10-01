#!/bin/sh
# REVIEW.md must find each planted bug: its file and a word for it.
f=REVIEW.md
fail() { echo "FAIL: $*"; exit 1; }
[ -f "$f" ] || fail "no $f"
for file in quota.go store.go handler.go; do
	grep -q "$file" "$f" || fail "$file not mentioned"
done
grep -iqE 'off.by.one|>= *(q\.)?limit|one (extra|more) request|limit *\+ *1|boundary|fencepost|allows? (one|1|an extra)' "$f" || fail "the off-by-one in Allow is not reported"
grep -iqE 'lock|mutex|data race|race condition|\brace\b|concurren|synchroni' "$f" || fail "the unlocked Increment is not reported"
grep -iqE 'swallow|ignor|discard|dropp|lost|silent|returns? nil|never (returned|propagated)|not (returned|propagated)' "$f" || fail "the swallowed Record error is not reported"
echo ok
