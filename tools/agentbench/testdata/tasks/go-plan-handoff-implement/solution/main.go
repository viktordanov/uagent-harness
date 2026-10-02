// Command shorty serves the URL shortener.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/shorty/internal/server"
	"example.com/shorty/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "the address to listen on")
	data := flag.String("data", "", "keep the links in this file, so they survive a restart (default: in memory)")
	flag.Parse()
	var st store.Store = store.NewMemory()
	if *data != "" {
		f, err := store.OpenFile(*data)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		st = f
	}
	srv := &http.Server{Addr: *addr, Handler: server.New(st), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("shorty listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
