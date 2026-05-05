package main

import (
	"log"
	"net/http"
	"os"

	"github.com/example/google-project-cost-manager/internal/app"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	s, err := app.NewServer(app.Options{})
	if err != nil {
		log.Fatalf("server init: %v", err)
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, s.Router()))
}
