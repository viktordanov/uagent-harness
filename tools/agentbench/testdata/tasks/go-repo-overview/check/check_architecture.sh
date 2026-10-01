#!/bin/sh
# Checks ARCHITECTURE.md for the facts a reader of the code must find.
f=ARCHITECTURE.md
fail() { echo "FAIL: $*"; exit 1; }
[ -f "$f" ] || fail "no $f"
size=$(wc -c < "$f")
[ "$size" -ge 3072 ] || fail "$f is $size bytes, want at least 3072"
for p in shipd api queue store notify; do
	grep -qiw "$p" "$f" || fail "package $p not described"
done
for s in Dispatcher MemStore Sender Job Server Enqueue; do
	grep -qw "$s" "$f" || fail "$s not named"
done
grep -iqE 'retr(y|ies|ied|ying).*(queue|dispatcher)|(queue|dispatcher).*retr(y|ies|ied|ying)' "$f" || fail "does not say the queue retries"
grep -iqE 'backoff|exponential' "$f" || fail "backoff not described"
grep -iqE 'dead.?letter' "$f" || fail "dead letters not described"
grep -iqE 'idempoten' "$f" || fail "idempotency keys not described"
grep -iqE 'hmac|signature|signs?\b|signed' "$f" || fail "webhook signing not described"
grep -iqE 'permanent|4xx|non.?retryable|Retryable' "$f" || fail "permanent failures not described"
grep -iq 'sms' "$f" || fail "SMS not addressed"
grep -iE 'sms' "$f" | grep -qE 'Sender|notify' || grep -iqE 'implement(s|ing)?[^.]*Sender' "$f" || fail "SMS answer does not point at notify.Sender"
echo ok
