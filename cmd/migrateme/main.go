package main

import (
	"github.com/amr0ny/migrateme/internal/commands"
	"log"
	"os"
)

func main() {
	cmd := commands.NewRootCommand()

	if err := cmd.Execute(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
