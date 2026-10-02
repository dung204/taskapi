package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dung204/taskapi/internal/httpapi"
)

func main() {
	config := loadConfig()
	handler := httpapi.NewHandler()

	server := &http.Server{
		Addr:              ":" + config.port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	err := server.ListenAndServe()
	if err != nil {
		fmt.Print(err)
	}
}
