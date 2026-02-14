package main

import (
	"context"
	"log"
	"os"
)

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)

	application := newApp()
	if err := application.Run(context.Background(), os.Args); err != nil {
		logger.Fatal(err)
	}
}
