// Package main is the entry point for the CloudPulse backend server.
//
// CloudPulse is a cloud task performance modeling and evaluation system
// that simulates multi-VM queueing dynamics on cloud task scheduling datasets.
//
// This binary starts an HTTP server on port 8080 that:
//   - Accepts CSV dataset uploads via POST /api/upload
//   - Processes a default workspace dataset via GET /api/process-default
//   - Serves generated HTML dashboard visualizations via /vizb/
//   - Serves the React frontend SPA build from ../frontend/dist/
//
// Usage:
//
//	cloudpulse-server         # Start the HTTP server on :8080
//	cloudpulse-server serve   # Same as above (serve is the default action)
//	cloudpulse-server -h      # Print usage information
package main

import (
	"fmt"
	"os"

	"cloudpulse-sim/server/internal/api"
)

func main() {
	// Handle the help flag: if the user passes -h or --help,
	// print a usage message and exit without starting the server.
	if len(os.Args) > 1 {
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Println("CloudPulse server entrypoint")
			fmt.Println("Usage: cloudpulse-server [serve]")
			return
		}
	}

	// Delegate to the API package which registers all HTTP routes
	// and starts listening on port 8080. This call blocks until
	// the server is shut down or encounters a fatal error.
	api.StartServer()
}
