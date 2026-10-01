// Command server serves the task API and the web app.
package main

import (
	"log"
	"net/http"
	"time"

	"example.com/tasks/api"
)

func main() {
	mux := http.NewServeMux()
	mux.Handle("/api/tasks", api.Handler{Store: api.NewStore(time.Now)})
	mux.Handle("/", http.FileServer(http.Dir("web")))
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", mux))
}
