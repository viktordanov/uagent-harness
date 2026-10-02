# gateway

The compute gateway: tenants call `POST /v1/compute` with a number of units,
and the gateway records their usage for billing.

```sh
go run ./cmd/gateway
```
