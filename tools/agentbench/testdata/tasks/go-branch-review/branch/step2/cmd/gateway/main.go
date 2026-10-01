// Command gateway serves the compute API.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"example.com/gateway/internal/quota"
	"example.com/gateway/internal/server"
	"example.com/gateway/internal/usage"
)

func main() {
	limit := flag.Int("quota", 1000, "requests per tenant per hour")
	flag.Parse()
	q := quota.New(*limit)
	go func() {
		for range time.Tick(time.Hour) {
			q.Reset()
		}
	}()
	h := server.NewHandler(usage.NewMemory(), q)
	log.Fatal(http.ListenAndServe(":8080", h))
}
