# shipd architecture

shipd accepts shipment status events over HTTP, keeps each shipment's history,
and tells subscribers about every new event through signed webhooks. Delivery
happens in the background, so an HTTP caller never waits for a subscriber.

## Packages

| Package | Owns |
| --- | --- |
| `cmd/shipd` | The binary: flags (`-addr`, `-workers`), wiring of the store, the sender, the dispatcher, and the HTTP server. |
| `internal/api` | The HTTP surface: routes, request validation, request IDs, idempotency keys, JSON errors. |
| `internal/store` | The `Store` interface and its in-memory implementation `MemStore`: shipments, events, subscribers. |
| `internal/queue` | Background delivery: the `Dispatcher`, its worker pool, retries with exponential backoff, and the dead-letter list. |
| `internal/notify` | Sending one message to one subscriber: the `Sender` interface, the `WebhookSender`, HMAC signing, and the split between retryable and permanent errors. |

### cmd/shipd

`main` builds a `store.NewMemStore()`, a `notify.NewWebhookSender` with an
`http.Client` that times out after 5 s, and a `queue.NewDispatcher` with
`Options{Workers: -workers, MaxAttempts: 5, BaseDelay: 200ms}`. It starts the
dispatcher, builds `api.NewServer(store, dispatcher)`, and serves
`srv.Routes()` with `http.ListenAndServe`. This is the only place that knows
the concrete types; everything else depends on interfaces or on the
dispatcher.

### internal/api

`Server` holds the `store.Store`, the `*queue.Dispatcher`, and an idempotency
cache. `Routes` registers three routes on a `ServeMux` and wraps them in
`withRequestID`, which keeps the caller's `X-Request-ID` or makes a random one
and echoes it in the response:

- `POST /v1/events` → `handleCreateEvent`
- `GET /v1/shipments/{id}` → `handleGetShipment`
- `POST /v1/subscriptions` → `handleSubscribe`

`validate` checks that `shipment_id` is set and that `status` is one of the
known statuses (`created`, `picked_up`, `in_transit`, `out_for_delivery`,
`delivered`, `exception`). `writeError` writes `{"error": "..."}` with a
status code.

The idempotency cache (`idempotency.go`) is an LRU of the last 10,000
`Idempotency-Key` header values, guarded by a mutex. A key is remembered only
after the event was stored and its notifications were queued.

### internal/store

`Store` is the interface the API uses: `AppendEvent`, `Shipment`,
`AddSubscriber`, `Subscribers`. `MemStore` implements it with maps behind an
`RWMutex`. `AppendEvent` gives each event a global, increasing `Seq` and the
current UTC time, creates the shipment on its first event, appends the event,
and sets the shipment's `Status` to the event's status. Reads (`Shipment`,
`Subscribers`) return copies, so a caller can never change data another
caller sees. An unknown shipment is `ErrNotFound`.

### internal/queue

`Dispatcher` owns a buffered channel of 1,024 `Job`s and `Workers`
goroutines started by `Start` and stopped by `Stop` (which cancels their
context; jobs still queued are dropped). `Enqueue` never blocks: if the
channel is full, the job goes straight to the dead-letter list with the
reason "queue full".

Each worker calls `deliver`, which sends the job once through the
`notify.Sender`. The queue, not the sender, is responsible for retries:

1. On success, the job is done.
2. On failure, the job's attempt count goes up. If it reached `MaxAttempts`
   (5 in `main`), or `notify.Retryable(err)` is false, the job goes to the
   dead-letter list and the failure is logged.
3. Otherwise the job is re-enqueued after `backoff(BaseDelay, attempt)`, an
   exponential backoff of `base·2^(attempt-1)` capped at 30 s, using
   `time.AfterFunc`. A retry that finds the queue full is dead-lettered too.

`DeadLetters` returns the jobs given up on, for an operator.

### internal/notify

`Sender` has one method, `Send(ctx, Message) error`, and must not retry.
`WebhookSender` marshals the event to JSON, signs the body with HMAC-SHA256
using the subscriber's secret (`sign`, sent as `X-Shipd-Signature:
sha256=<hex>`), and POSTs it. A 5xx answer or a transport error is a plain
error, so it is retryable. A 4xx answer, or a body or request that cannot be
built, is a `*PermanentError`; `Retryable` returns false for it, so the queue
dead-letters the job instead of retrying.

## Request flow: POST /v1/events

1. `withRequestID` sets `X-Request-ID`.
2. `handleCreateEvent` reads `Idempotency-Key`. If the key was seen before,
   it answers 200 with no body and does nothing else.
3. It decodes the JSON body into `eventRequest` (400 on bad JSON) and runs
   `validate` (422 on a missing ID or an unknown status).
4. `store.AppendEvent` saves the event, numbering and timestamping it, and
   updates the shipment's status (500 if the store fails).
5. `store.Subscribers` lists the shipment's webhooks; for each, the handler
   calls `Dispatcher.Enqueue(queue.Job{URL, Secret, Event})`. A failure to
   list subscribers is ignored: the event is stored, but nobody is told.
6. The key is remembered, and the handler answers 201 with the saved event.
7. Later, a dispatcher worker sends each job through `WebhookSender.Send`.

### When a webhook delivery fails

- 5xx or a network error: retried with exponential backoff (200 ms, 400 ms,
  800 ms, 1.6 s) until the 5th attempt fails, then dead-lettered.
- 4xx: a `PermanentError`, dead-lettered at once without a retry.
- Queue full at enqueue or at a retry: dead-lettered with "queue full".
- Shutdown: jobs still in the channel are dropped.

## Adding SMS notifications

SMS is another delivery channel, so it belongs in `internal/notify`:

1. Add an `SMSSender` type in `internal/notify` that implements `Sender`
   (`Send(ctx, Message) error`), sending once and returning a
   `*PermanentError` for failures that retrying cannot fix (for example an
   invalid number), so the queue's retry, backoff, and dead-letter handling
   apply unchanged.
2. Subscribers need to say how to reach them: extend `store.Subscriber`
   (today only `URL` and `Secret`) with a channel and a phone number, and
   `handleSubscribe`'s validation with it.
3. Choose the sender per job: either carry the channel on `queue.Job` and give
   the `Dispatcher` a sender per channel, or wrap both senders in a small
   routing `Sender` in `internal/notify` that picks one by the message, so the
   dispatcher keeps a single `Sender`.
4. Wire the new sender in `cmd/shipd/main.go`, next to `NewWebhookSender`.

The queue itself does not change: retries stay in one place.
