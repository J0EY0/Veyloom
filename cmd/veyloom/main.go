// Command veyloom is the entry point for both the hub and the worker.
//
// At this stage it only offers engine discovery on the local machine:
//
//	veyloom discover          print the engines found on this machine
//	veyloom serve             expose the same information over HTTP
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Ctrl-C or SIGTERM cancels the context every command receives, so
	// long-running commands such as serve can shut down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		// Cobra has already printed the error.
		os.Exit(1)
	}
}
