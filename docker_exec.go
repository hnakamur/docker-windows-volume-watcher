package watcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type statusError struct {
	StatusCode int
}

func (e statusError) Error() string {
	return fmt.Sprintf("Code: %d", e.StatusCode)
}

// dockerExec is simplified version of docker exec without -it option.
func dockerExec(ctx context.Context, cli *client.Client, containerID string, cmd []string, stdout, stderr io.Writer) error {
	if _, err := cli.ContainerInspect(ctx, containerID); err != nil {
		return err
	}

	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStderr: true,
		AttachStdout: true,
	}
	respCreate, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return err
	}
	execID := respCreate.ID
	if execID == "" {
		return errors.New("exec ID empty")
	}

	execStartCheck := types.ExecStartCheck{}
	resp, err := cli.ContainerExecAttach(ctx, execID, execStartCheck)
	if err != nil {
		return err
	}
	defer resp.Close()

	errC := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, resp.Reader)
		errC <- err
	}()

	select {
	case err := <-errC:
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to docker exec, err=%v\n", err)
		}
	case <-ctx.Done():
		fmt.Fprintf(os.Stderr, "docker exec canceled\n")
	}

	return getExecExitStatus(ctx, cli, execID)
}

func getExecExitStatus(ctx context.Context, cli *client.Client, execID string) error {
	resp, err := cli.ContainerExecInspect(ctx, execID)
	if err != nil {
		// If we can't connect, then the daemon probably died.
		if !client.IsErrConnectionFailed(err) {
			return err
		}
		return statusError{StatusCode: -1}
	}
	status := resp.ExitCode
	if status != 0 {
		return statusError{StatusCode: status}
	}
	return nil
}
