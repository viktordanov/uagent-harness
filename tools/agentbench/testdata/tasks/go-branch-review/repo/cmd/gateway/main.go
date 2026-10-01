// Command gateway serves the compute API.
package main

import (
	"log"
	"net/http"

	"example.com/gateway/internal/server"
	"example.com/gateway/internal/usage"
)

func main() {
	h := server.NewHandler(usage.NewMemory())
	log.Fatal(http.ListenAndServe(":8080", h))
}
