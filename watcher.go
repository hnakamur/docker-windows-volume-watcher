package watcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stringid"
	"github.com/fsnotify/fsnotify"
)

type watcher struct {
	hostDirWatcher *fsnotify.Watcher
	cli            *client.Client
}

type mountPoint struct {
	containerID  string
	hostDir      string
	containerDir string
}

func Watch(ctx context.Context, apiVersion, hostDir string) error {
	err := validateHostDir(hostDir)
	if err != nil {
		return err
	}
	hostDir = cleanHostDir(hostDir)

	log.Printf("hostDir=%s", hostDir)

	w, err := newWatcher(apiVersion)
	if err != nil {
		return err
	}
	return w.Watch(ctx, hostDir)
}

func newWatcher(apiVersion string) (*watcher, error) {
	hostDirWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	cli, err := client.NewClientWithOpts(client.WithVersion(apiVersion), client.FromEnv)
	if err != nil {
		return nil, err
	}
	log.Printf("started docker client")

	return &watcher{
		hostDirWatcher: hostDirWatcher,
		cli:            cli,
	}, nil
}

func (w *watcher) Watch(ctx context.Context, hostDir string) error {
	mountPoints, err := w.listMountPoints(ctx, hostDir)
	if err != nil {
		return err
	}

	err = w.addWatchDirs(hostDir)
	if err != nil {
		return err
	}
	log.Printf("started hostDirWatcher")

	msgC, errC := watchContainerEvents(ctx, w.cli)
	for {
		select {
		case hostDirEvent := <-w.hostDirWatcher.Events:
			switch hostDirEvent.Op {
			case fsnotify.Create:
				log.Printf("hostDirWatcher create event.Name=%s", hostDirEvent.Name)
				err := w.mayAddWatchDir(hostDirEvent.Name)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			case fsnotify.Remove:
				log.Printf("hostDirWatcher remove event.Name=%s", hostDirEvent.Name)
				// NOTE: We don't need to remove watch manually. We got an error if we did.
			case fsnotify.Rename:
				log.Printf("hostDirWatcher rename event.Name=%s", hostDirEvent.Name)
				// NOTE: We don't need to remove watch manually. We got an error if we did.
			case fsnotify.Write:
				err := w.notifyWriteToContainer(ctx, mountPoints, hostDirEvent.Name)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			}
		case err := <-w.hostDirWatcher.Errors:
			fmt.Fprintln(os.Stderr, err)
		case msg := <-msgC:
			//log.Printf("msg=%+v", msg)
			switch msg.Action {
			case "start", "restart", "die", "pause", "unpause":
				mountPoints, err = w.listMountPoints(ctx, hostDir)
				if err != nil {
					return err
				}
			}
		case err := <-errC:
			fmt.Fprintln(os.Stderr, err)
		case <-ctx.Done():
			err := w.hostDirWatcher.Close()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			log.Printf("closed hostDirWatcher")

			err = w.cli.Close()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			log.Printf("closed docker client")
			return nil
		}
	}
}

func watchContainerEvents(ctx context.Context, cli *client.Client) (<-chan events.Message, <-chan error) {
	options := types.EventsOptions{
		Filters: filters.NewArgs(filters.Arg("type", "container")),
	}
	return cli.Events(ctx, options)
}

func (w *watcher) addWatchDirs(hostDir string) error {
	watchDir := func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if strings.HasPrefix(fi.Name(), ".") {
				return filepath.SkipDir
			}

			if err := w.hostDirWatcher.Add(path); err != nil {
				return err
			}
		}
		return nil
	}
	return filepath.Walk(hostDir, watchDir)
}

func (w *watcher) mayAddWatchDir(hostDir string) error {
	ok, err := isDir(hostDir)
	if err != nil {
		return err
	}

	if !ok {
		return nil
	}
	err = w.hostDirWatcher.Add(hostDir)
	if err != nil {
		return fmt.Errorf("failed to add watch dir=%s, err=%v", hostDir, err)
	}
	log.Printf("added watch hostdir=%s", hostDir)
	return nil
}

func isDir(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return fi.IsDir(), nil
}

func (w *watcher) listMountPoints(ctx context.Context, hostDir string) ([]mountPoint, error) {
	options := types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("status", "created"),
			filters.Arg("status", "restarting"),
			filters.Arg("status", "running"),
			filters.Arg("status", "paused"),
		),
	}
	containers, err := w.cli.ContainerList(ctx, options)
	if err != nil {
		return nil, err
	}
	var mountPoints []mountPoint
	for _, c := range containers {
		//log.Printf("container=%+v", c)
		for _, m := range c.Mounts {
			mp := mountPoint{
				containerID:  c.ID,
				hostDir:      convertDockerMountSourceDirToHostDir(m.Source),
				containerDir: m.Destination,
			}
			if hasPrefixFilePath(hostDir, mp.hostDir) {
				err := verifyContainerHavingStatAndChmod(ctx, w.cli, mp.containerID)
				if err != nil {
					log.Printf("skip container id=%s since it is not running or does not have \"stat\" or \"chmod\" command, err=%v\n",
						stringid.TruncateID(mp.containerID), err)
					continue
				}
				mountPoints = append(mountPoints, mp)
			}
		}
	}

	if len(mountPoints) == 0 {
		log.Printf("no watching mount")
	} else {
		for i, mp := range mountPoints {
			log.Printf("watching mount[%d]: containerID=%s, hostDir=%s, containerDir=%s",
				i, stringid.TruncateID(mp.containerID), mp.hostDir, mp.containerDir)
		}
	}

	return mountPoints, nil
}

func (w *watcher) notifyWriteToContainer(ctx context.Context, mountPoints []mountPoint, hostPath string) error {
	for _, mp := range mountPoints {
		if !hasPrefixFilePath(hostPath, mp.hostDir) {
			continue
		}

		containerPath := convertHostPathToContainerPath(
			mp.hostDir, mp.containerDir, hostPath)
		var buf bytes.Buffer
		err := dockerExec(ctx, w.cli, mp.containerID,
			[]string{"stat", "-c", "%a", containerPath},
			&buf, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip notify since stat failed; err=%s\n", err)
			continue
		}
		perm := strings.TrimSuffix(buf.String(), "\n")
		err = dockerExec(ctx, w.cli, mp.containerID,
			[]string{"chmod", perm, containerPath},
			ioutil.Discard, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to notify since chmod failed; err=%s\n", err)
			continue
		}

		log.Printf("notified to container %s, hostPath=%s, containerPath=%s\n",
			stringid.TruncateID(mp.containerID), hostPath, containerPath)
	}
	return nil
}

func verifyContainerHavingStatAndChmod(ctx context.Context, cli *client.Client, containerID string) error {
	var wg sync.WaitGroup
	wg.Add(2)

	var statErr, chmodErr error
	go func() {
		defer wg.Done()
		statErr = dockerExec(ctx, cli, containerID, []string{"stat", "--help"}, ioutil.Discard, ioutil.Discard)
	}()

	go func() {
		defer wg.Done()
		chmodErr = dockerExec(ctx, cli, containerID, []string{"chmod", "--help"}, ioutil.Discard, ioutil.Discard)
	}()

	wg.Wait()
	if statErr != nil {
		return statErr
	}
	return chmodErr
}

func validateHostDir(hostDir string) error {
	if !filepath.IsAbs(hostDir) {
		return errors.New("hostdir must be an absolute path")
	}
	volName := filepath.VolumeName(hostDir)
	if len(volName) != 2 || volName[1] != ':' {
		return errors.New("hostdir must not be a network path")
	}

	fi, err := os.Lstat(hostDir)
	if err != nil {
		return fmt.Errorf("hostdir must exist; err=%v", err)
	}
	if !fi.IsDir() {
		return errors.New("hostdir must be a directory")
	}
	return nil
}

func cleanHostDir(hostDir string) string {
	hostDir = filepath.Clean(hostDir)
	return strings.ToUpper(hostDir[:1]) + hostDir[1:]
}

func convertDockerMountSourceDirToHostDir(source string) string {
	if !strings.HasPrefix(source, "/host_mnt/") {
		panic(fmt.Sprintf("unexpected docker mount source directory path format, source=%s", source))
	}
	p := source[len("/host_mnt/"):]
	return strings.ToUpper(p[:1]) + ":" + strings.Replace(p[1:], "/", string(filepath.Separator), -1)
}

func convertHostPathToContainerPath(hostDir, containerDir, hostPath string) string {
	rel, err := filepath.Rel(hostDir, hostPath)
	if err != nil {
		panic(fmt.Sprintf("unexpected error for getting relative path, basepath=%s, targpath=%s, err=%s\n", hostDir, hostPath, err))
	}
	return path.Join(containerDir, filepath.ToSlash(rel))
}
