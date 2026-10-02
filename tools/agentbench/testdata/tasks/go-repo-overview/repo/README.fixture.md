# shipd

shipd accepts shipment events over HTTP, stores them, and tells subscribers
about them through webhooks.

```sh
go run ./cmd/shipd -addr :8080
```
