package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	watcher "github.com/hnakamur/docker-windows-volume-watcher"
)

func main() {
	hostDir := flag.String("hostdir", "", "host directory to watch")
	apiVer := flag.String("apiver", "1.38", "Docker API version")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	c := make(chan os.Signal, 1)
	signal.Notify(c)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-c
		log.Printf("received signal")
		cancel()
		log.Printf("called cancel")
	}()

	err := watcher.Watch(ctx, *apiVer, *hostDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
