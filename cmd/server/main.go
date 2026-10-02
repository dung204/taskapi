package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
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
	if !errors.Is(err, http.ErrServerClosed) {
		fmt.Print(err)
		os.Exit(1)
	}
}
