package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/nandansrikrishna/jev-go/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if ctx.Err() != nil {
		code = 130
	}
	os.Exit(code)
}
