# inventory-sync

Snapshots of the warehouse inventory API, kept in `data/` so the monthly
audit can diff them.

- `data/items.json`: every item, as the API returns it, sorted by `id`.
- `data/audit.json`: the details record of each item flagged `audit: true`,
  sorted by `id`.

The API pages its item list (`/v1/items?page=N`, each page says the `next`
page or `null`); details are at `/v1/items/{id}/details`.
