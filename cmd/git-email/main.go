package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/james-yusuke/git-email/internal/askpass"
	"github.com/james-yusuke/git-email/internal/cli"
)

func main() {
	if askpass.Active() {
		if err := askpass.Run(os.Args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
