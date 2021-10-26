package main

import (
	"context"

	"github.com/google/martian/log"

	"github.com/erda-project/erda-sourcecov/agent/core"
)

func main() {
	log.SetLevel(log.Info)
	ctx, _ := context.WithCancel(context.Background())

	core.WatchJacocoPod(ctx)
	go core.WatchJob(ctx)
	select {
	case <-ctx.Done():
	}
}
