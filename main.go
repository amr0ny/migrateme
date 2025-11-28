package main

import (
	"github.com/amr0ny/migrateme/internal/cli"
	"log"
	"os"
)

func main() {
	cmd := cli.NewRootCommand()

	if err := cmd.Execute(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
