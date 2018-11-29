docker-windows-volume-watcher
=============================

A simple command line utility to get autobuild working inside containers running on Docker for Windows
with mounting host (= Windows) directories to containers.

This is a workaround for [Inotify on shared drives does not work · Issue #56 · docker/for-win](https://github.com/docker/for-win/issues/56).

Inspired by [Mikhail Erofeev's Python tool](https://github.com/merofeev/docker-windows-volume-watcher)
and [Frode Hus's Go tool](https://github.com/FrodeHus/docker-windows-volume-watcher).

## Usage

Run the command `docker-windows-volume-watcher` on any directory you like.

Pass semicolon separated directories that you want to ignore to `-ignoredir` option.

You can specify relative directories like below, in this case the directoies are converted
to the absolute path with a mount source directory as the base directory. 

```
docker-windows-volume-watcher -ignoredir .git;build\html
```

Also you can specifiy absolute directories like `-ignoredir C:\path\to\ignore`.
And you can mix relative and absolute directories `ignoredir .git;build\html;C:\path\to\ignore`.

## Limitations

The script doesn't propagate to container file deletion events.
The script requires `stat` and `chmod` utils to be installed in container (this should be true by default for the most of containers).

## Implementation details

This command uses [a Go client for the Docker Engine API](https://godoc.org/github.com/docker/docker/client) to observe container lifecycle events.

It also uses [fsnotify/fsnotify: Cross-platform file system notifications for Go.](https://github.com/fsnotify/fsnotify) to observe file change events of the host directory. Once file change event is fired the script reads file permissions of changed file (using stat util) and rewrites file permissions with the same value (using chmod util) thus triggering inotify event inside container.

"Rewrite file permissions approach" was used instead of updating file last modified time with touch util. Since touching will cause event loop: touch will trigger file change event in Windows, script will handle this event and touch file again, etc.
