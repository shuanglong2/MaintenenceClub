package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := os.Getenv("MCLUB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	server := new(http.Server)
	server.Addr = addr
	server.Handler = routes()
	server.ReadHeaderTimeout = 5 * time.Second

	log.Printf("listen %s", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", home)
	return mux
}

func home(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("维修组\n"))
}
