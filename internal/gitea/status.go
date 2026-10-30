package gitea

import (
	"context"
	"time"

	gitea "gitea.dev/sdk"
)

var StatusPollInterval = 5 * time.Second

const MaxStatusWaitSeconds = 45 // stays under the 60s default request timeout of common MCP clients

func WaitStatus(ctx context.Context, waitSeconds int64, poll func() (*gitea.CombinedStatus, error)) (*gitea.CombinedStatus, error) {
	deadline := time.Now().Add(time.Duration(max(0, min(waitSeconds, MaxStatusWaitSeconds))) * time.Second)
	for {
		status, err := poll()
		if err != nil {
			return nil, err
		}
		if (status.State != "" && status.State != gitea.StatusPending) || !time.Now().Before(deadline) {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(min(StatusPollInterval, time.Until(deadline))):
		}
	}
}
