package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/abdi27s/attend-sync/internal/api"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	api.RegisterRoutes(mux)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	fmt.Printf("Attend-Sync API running on http://localhost:%s\n", port)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
