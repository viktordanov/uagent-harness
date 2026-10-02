// Command bookmarks serves the bookmark API.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/bookmarks/internal/handlers"
	"example.com/bookmarks/internal/seed"
	"example.com/bookmarks/internal/store"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "listen address")
	demo := flag.Bool("seed", false, "start with a few demo bookmarks")
	flag.Parse()

	logger := log.New(os.Stderr, "bookmarks: ", log.LstdFlags)
	s := store.New()
	if *demo {
		if err := seed.Load(s); err != nil {
			logger.Fatal(err)
		}
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handlers.Logging(handlers.New(s), logger),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Printf("listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		logger.Fatal(err)
	}
}
