// Command statusapi is the fake status API the curl-parallel-endpoints task
// talks to: four independent endpoints of different shapes, each slow
// enough that fetching them at once pays.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// delay is each endpoint's response time.
const delay = 1500 * time.Millisecond

func main() {
	endpoints := map[string]any{
		"/v1/regions": []map[string]any{
			{"name": "eu-west", "healthy": true, "latency_ms": 41},
			{"name": "us-east", "healthy": true, "latency_ms": 23},
			{"name": "ap-south", "healthy": false, "latency_ms": 310},
		},
		"/v1/deploys/latest": map[string]any{"service": "checkout", "version": "4.12.3", "deployed_at": "2026-09-30T14:02:11Z", "by": "release-bot"},
		"/v1/incidents": map[string]any{"open": []map[string]any{
			{"id": "INC-2207", "severity": "sev2", "title": "ap-south elevated latency"},
		}, "resolved_this_week": 4},
		"/v1/flags": map[string]bool{"new_checkout": true, "dark_mode": false, "bulk_export": true},
	}
	mux := http.NewServeMux()
	for path, body := range endpoints {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(delay)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(body)
		})
	}
	log.Fatal(http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), mux))
}
