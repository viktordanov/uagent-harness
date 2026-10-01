# INC-2291 – Ravi Shah (payments eng)

PR #4412 was meant to raise `max_idle_conns` for the new region's traffic
pattern and shorten `conn_max_lifetime` because of the new proxy. I edited
the wrong line: `DB_MAX_OPEN_CONNS` went from 200 to 20 and
`DB_MAX_IDLE_CONNS` stayed 50 (which is nonsense, idle > open, and nothing
caught it). Review approved it because the diff looks like a tuning change.

Why it only hurt prod-eu: the overlay is prod-eu only. prod-us kept 200.

With 12 pods × 20 = 240 connections total we should still have been fine on
paper, but each request holds a connection for the whole checkout
transaction (~300-400 ms under load), so at ~1,000 req/min with bursts the
pool saturated and requests queued.

## Action items

- Ravi Shah: CI validation for deploy overlays: reject `max_idle_conns` >
  `max_open_conns`, and flag any pool size change > 50% for a second reviewer.
- Mei Lin (platform): config-only changes go through the canary stage like
  code (today they skip it), so this would have hit 1 pod, not 12.
- Ravi Shah: shorten the checkout transaction: the fraud score call happens
  inside the DB transaction and doesn't need to.
