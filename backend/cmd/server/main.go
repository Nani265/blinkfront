package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"copilot-local-api/internal/server"
	"copilot-local-api/internal/store"
)

func main() {
	dataPath := os.Getenv("DATA_PATH")
	if dataPath == "" {
		// SQLite database file under backend/storage/
		dataPath = filepath.Join("storage", "database.sqlite")
	}
	st, err := store.Open(dataPath)
	if err != nil {
		log.Fatal(err)
	}
	port := "8081"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}
	apiHost := os.Getenv("API_HOST")
	if apiHost == "" {
		apiHost = os.Getenv("HOST")
	}
	addr := ":" + port
	displayHost := "127.0.0.1"
	if apiHost != "" {
		addr = net.JoinHostPort(apiHost, port)
		displayHost = apiHost
	}
	h := server.New(st)
	log.Printf("Copilot local API listening on http://%s/api (persisting to %s)", net.JoinHostPort(displayHost, port), dataPath)
	log.Printf("Fresh empty database seed logins: admin@sapience.com / password123 , fleet@sapience.com / password123")
	log.Fatal(http.ListenAndServe(addr, h))
}
