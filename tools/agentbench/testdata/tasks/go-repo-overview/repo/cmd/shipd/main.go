// Command shipd runs the shipment event service.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"example.com/shipd/internal/api"
	"example.com/shipd/internal/notify"
	"example.com/shipd/internal/queue"
	"example.com/shipd/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	workers := flag.Int("workers", 4, "notification workers")
	flag.Parse()

	st := store.NewMemStore()
	sender := notify.NewWebhookSender(&http.Client{Timeout: 5 * time.Second})
	d := queue.NewDispatcher(sender, queue.Options{Workers: *workers, MaxAttempts: 5, BaseDelay: 200 * time.Millisecond})
	d.Start()
	defer d.Stop()

	srv := api.NewServer(st, d)
	log.Printf("shipd listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Routes()))
}
