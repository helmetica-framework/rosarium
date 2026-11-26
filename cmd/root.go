// Package cmd holds the rosarium CLI.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "rosarium",
	Short:         "Gathers the Helmetica docs into docs.helmetica.io",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the CLI.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR", err)
		os.Exit(1)
	}
}
