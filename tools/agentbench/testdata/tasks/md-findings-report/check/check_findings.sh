#!/bin/sh
# docs/FINDINGS.md must be 10-25 KB and carry the facts the sources give.
f=docs/FINDINGS.md
fail() { echo "FAIL: $*"; exit 1; }
[ -f "$f" ] || fail "no $f"
size=$(wc -c < "$f")
[ "$size" -ge 10240 ] || fail "$f is $size bytes, want at least 10240"
[ "$size" -le 25600 ] || fail "$f is $size bytes, want at most 25600"
grep -iqE 'max_open_conns|DB_MAX_OPEN_CONNS|max open conn' "$f" || fail "root cause setting not named"
grep -iqE 'pool' "$f" || fail "connection pool exhaustion not described"
grep -qE '200 *(→|->|to) *20\b|from 200 to 20' "$f" || fail "the 200 to 20 change is not stated"
grep -qE '1,?284' "$f" || fail "total failed requests (1,284) missing"
grep -qE '8,?730' "$f" || fail "peak p99 (8,730 ms) missing"
grep -qE 'Petrova' "$f" || fail "Ana Petrova's actions missing"
grep -qE 'Shah' "$f" || fail "Ravi Shah's actions missing"
grep -qE 'Mei Lin' "$f" || fail "Mei Lin's action missing"
grep -iqE 'red herring|unrelated|not related|coincid' "$f" || fail "the Redis alert is not set aside"
echo ok
