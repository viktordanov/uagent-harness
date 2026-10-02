# ops-snapshot

A daily snapshot of the ops status API, kept in `data/` so the weekly
review can see what changed.

`data/snapshot.json` is one JSON object with a key per endpoint, each the
endpoint's response exactly as the API returns it:

| Key | Endpoint |
| --- | --- |
| `regions` | `/v1/regions` |
| `deploy` | `/v1/deploys/latest` |
| `incidents` | `/v1/incidents` |
| `flags` | `/v1/flags` |
