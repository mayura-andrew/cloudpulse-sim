package main

import (
	"fmt"
	"os"

	"cloudpulse-sim/server/internal/api"
)

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Println("CloudPulse server entrypoint")
			fmt.Println("Usage: cloudpulse-server [serve]")
			return
		}
	}

	api.StartServer()
}
