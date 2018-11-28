package watcher

import (
	"bytes"
	"context"
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
	ignoreDirs     []string
	mountPoints    map[string][]mountPoint
	cli            *client.Client
}

type mountPoint struct {
	hostDir      string
	containerDir string
	ignoreDirs   []string
}

// Watch watches changes of host files and notify the change to the container.
func Watch(ctx context.Context, apiVersion string, ignoreDirs []string) error {
	w, err := newWatcher(apiVersion, ignoreDirs)
	if err != nil {
		return err
	}
	return w.Watch(ctx)
}

func newWatcher(apiVersion string, ignoreDirs []string) (*watcher, error) {
	hostDirWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	cli, err := client.NewClientWithOpts(client.WithVersion(apiVersion), client.FromEnv)
	if err != nil {
		return nil, err
	}

	return &watcher{
		hostDirWatcher: hostDirWatcher,
		ignoreDirs:     ignoreDirs,
		mountPoints:    make(map[string][]mountPoint),
		cli:            cli,
	}, nil
}

func (w *watcher) Watch(ctx context.Context) error {
	err := w.buildInitialMountPoints(ctx)
	if err != nil {
		return err
	}

	for containerID, mountPoints := range w.mountPoints {
		if len(mountPoints) == 0 {
			continue
		}

		err = verifyContainerHavingStatAndChmod(ctx, w.cli, containerID)
		if err != nil {
			log.Printf(`Skip containerID=%s since it does not have "stat" or "chmod" command`, stringid.TruncateID(containerID))
			return nil
		}

		err = w.addWatchDirsForMountPoints(containerID, mountPoints)
		if err != nil {
			return err
		}
	}

	msgC, errC := watchContainerEvents(ctx, w.cli)
	for {
		select {
		case hostDirEvent := <-w.hostDirWatcher.Events:
			switch hostDirEvent.Op {
			case fsnotify.Create:
				err := w.mayAddWatchDir(hostDirEvent.Name)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			case fsnotify.Write:
				err := w.notifyWriteToContainer(ctx, hostDirEvent.Name)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			}
		case err := <-w.hostDirWatcher.Errors:
			fmt.Fprintln(os.Stderr, err)
		case msg := <-msgC:
			//log.Printf("msg=%+v", msg)
			containerID := msg.Actor.ID
			switch msg.Action {
			case "start", "restart", "unpause":
				mountPoints, err := w.getMountPoints(ctx, containerID)
				if err != nil {
					return err
				}
				if len(mountPoints) > 0 {
					err = verifyContainerHavingStatAndChmod(ctx, w.cli, containerID)
					if err != nil {
						log.Printf(`Skip containerID=%s since it does not have "stat" or "chmod" command`, stringid.TruncateID(containerID))
						return nil
					}
					err = w.addWatchDirsForMountPoints(containerID, mountPoints)
					if err != nil {
						return err
					}
					w.mountPoints[containerID] = mountPoints
				}

			case "die", "pause":
				mountPoints := w.mountPoints[containerID]
				if len(mountPoints) > 0 {
					err = w.removeWatchDirsForMountPoints(containerID, mountPoints)
					if err != nil {
						return err
					}
					delete(w.mountPoints, containerID)
				}
			}
		case err := <-errC:
			fmt.Fprintln(os.Stderr, err)
		case <-ctx.Done():
			err := w.hostDirWatcher.Close()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}

			err = w.cli.Close()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			return nil
		}
	}
}

func (w *watcher) buildInitialMountPoints(ctx context.Context) error {
	options := types.ContainerListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("status", "created"),
			filters.Arg("status", "restarting"),
			filters.Arg("status", "running"),
		),
	}
	containers, err := w.cli.ContainerList(ctx, options)
	if err != nil {
		return err
	}

	for _, c := range containers {
		mountPoints := w.convertMounts(c.Mounts)
		if len(mountPoints) > 0 {
			w.mountPoints[c.ID] = mountPoints
		}
	}

	return nil
}

func (w *watcher) convertMounts(mounts []types.MountPoint) []mountPoint {
	mountPoints := make([]mountPoint, len(mounts))
	for i, m := range mounts {
		hostDir := convertDockerMountSourceDirToHostDir(m.Source)
		var ignoreDirs []string
		for _, ignoreDir := range w.ignoreDirs {
			if filepath.IsAbs(ignoreDir) {
				if hasPrefixFilePath(ignoreDir, hostDir) {
					ignoreDirs = append(ignoreDirs, ignoreDir)
				}
			} else {
				ignoreDirs = append(ignoreDirs, filepath.Join(hostDir, ignoreDir))
			}
		}

		mountPoints[i] = mountPoint{
			hostDir:      hostDir,
			containerDir: m.Destination,
			ignoreDirs:   ignoreDirs,
		}
	}
	return mountPoints
}

func (w *watcher) getMountPoints(ctx context.Context, containerID string) ([]mountPoint, error) {
	res, err := w.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}
	return w.convertMounts(res.Mounts), nil
}

func watchContainerEvents(ctx context.Context, cli *client.Client) (<-chan events.Message, <-chan error) {
	options := types.EventsOptions{
		Filters: filters.NewArgs(filters.Arg("type", "container")),
	}
	return cli.Events(ctx, options)
}

func (w *watcher) addWatchDirsForMountPoints(containerID string, mountPoints []mountPoint) error {
	for i, mp := range mountPoints {
		err := w.addWatchDirs(mp)
		if err != nil {
			return err
		}
		log.Printf("watching mount[%d]: containerID=%s, hostDir=%s",
			i, stringid.TruncateID(containerID), mp.hostDir)
	}
	return nil
}

func (w *watcher) addWatchDirs(mp mountPoint) error {
	watchDir := func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return nil
		}

		if mp.IgnoredPath(path) {
			log.Printf("skip add watch dir=%s", path)
			return filepath.SkipDir
		}

		if err := w.hostDirWatcher.Add(path); err != nil {
			return err
		}
		log.Printf("added watch dir=%s", path)

		return nil
	}
	return filepath.Walk(mp.hostDir, watchDir)
}

func (w *watcher) removeWatchDirsForMountPoints(containerID string, mountPoints []mountPoint) error {
	for i, mp := range mountPoints {
		err := w.removeWatchDirs(mp)
		if err != nil {
			return err
		}
		log.Printf("unwatching mount[%d]: containerID=%s, hostDir=%s",
			i, stringid.TruncateID(containerID), mp.hostDir)
	}
	return nil
}

func (w *watcher) removeWatchDirs(mp mountPoint) error {
	watchDir := func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return nil
		}

		if mp.IgnoredPath(path) {
			log.Printf("skip remove watch dir=%s", path)
			return filepath.SkipDir
		}

		if err := w.hostDirWatcher.Remove(path); err != nil {
			// NOTE: We ignore error here since we may call Remove for path
			// which we had not call Add.
			return nil
		}
		log.Printf("removed watch dir=%s", path)

		return nil
	}
	return filepath.Walk(mp.hostDir, watchDir)
}

func (w *watcher) mayAddWatchDir(hostDir string) error {
	fi, err := os.Stat(hostDir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return nil
	}

	cid, mp := w.lookupMountPoint(hostDir)
	if cid == "" {
		return nil
	}

	if mp.IgnoredPath(hostDir) {
		return nil
	}

	err = w.hostDirWatcher.Add(hostDir)
	if err != nil {
		return fmt.Errorf("failed to add watch dir=%s, err=%v", hostDir, err)
	}
	log.Printf("added watch hostdir=%s", hostDir)
	return nil
}

func (w *watcher) lookupMountPoint(hostPath string) (string, mountPoint) {
	for cid, mountPoints := range w.mountPoints {
		for _, mp := range mountPoints {
			if hasPrefixFilePath(hostPath, mp.hostDir) {
				return cid, mp
			}
		}
	}
	return "", mountPoint{}
}

func (w *watcher) notifyWriteToContainer(ctx context.Context, hostPath string) error {
	containerID, mp := w.lookupMountPoint(hostPath)
	if containerID == "" {
		return nil
	}

	containerPath := convertHostPathToContainerPath(
		mp.hostDir, mp.containerDir, hostPath)

	if mp.IgnoredPath(hostPath) {
		return nil
	}

	var buf bytes.Buffer
	err := dockerExec(ctx, w.cli, containerID,
		[]string{"stat", "-c", "%a", containerPath},
		&buf, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skip notify since stat failed; err=%s\n", err)
		return nil
	}
	perm := strings.TrimSuffix(buf.String(), "\n")
	err = dockerExec(ctx, w.cli, containerID,
		[]string{"chmod", perm, containerPath},
		ioutil.Discard, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to notify since chmod failed; err=%s\n", err)
		return nil
	}

	log.Printf("notified to container %s, hostPath=%s, containerPath=%s\n",
		stringid.TruncateID(containerID), hostPath, containerPath)
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

func (mp *mountPoint) IgnoredPath(path string) bool {
	for _, ignoreDir := range mp.ignoreDirs {
		if hasPrefixFilePath(path, ignoreDir) {
			return true
		}
	}
	return false
}
