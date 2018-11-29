package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	watcher "github.com/hnakamur/docker-windows-volume-watcher"
)

func main() {
	ignoreDir := flag.String("ignoredir", "", `ignore directories, semilocon separated directories. relative to mount point (e.g. build\html) or absolute`)
	apiVer := flag.String("apiver", "1.38", "Docker API version")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	c := make(chan os.Signal, 1)
	signal.Notify(c)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-c
		cancel()
	}()

	var ignoreDirs []string
	if *ignoreDir != "" {
		ignoreDirs = strings.Split(*ignoreDir, ";")
	}
	err := watcher.Watch(ctx, *apiVer, ignoreDirs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
