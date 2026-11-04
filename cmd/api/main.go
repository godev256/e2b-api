// Command api runs the sandbox API service and its maintenance commands.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/buildoshi/e2b-api/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first signal restore default handling, so a second Ctrl-C
	// ends the process at once instead of waiting for the drain.
	go func() {
		<-ctx.Done()
		stop()
	}()

	var command string
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if err := app.Run(ctx, command); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
