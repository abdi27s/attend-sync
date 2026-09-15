package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/abdi27s/attend-sync/internal/api"
)

func main() {
	mux := http.NewServeMux()

	api.RegisterRoutes(mux)

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	fmt.Println("Attend-Sync API running on http://localhost:8080")

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
