package syncutil

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// convenience wrapper around errgroup to run a simple task concurrently, and stop each
// worker if any of the tasks encounter an error during run
func Concurrently(
	ctx context.Context,
	concurrency int,
	task func(ctx context.Context) error,
	workSubmitter func(taskCtx context.Context) error,
) error {
	// if any of the workers error, taskCtx will be canceled.
	// taskCtx will also be canceled if parent ctx cancels.
	errGroup, taskCtx := errgroup.WithContext(ctx)

	for i := 0; i < concurrency; i++ {
		errGroup.Go(func() error {
			return task(taskCtx)
		})
	}

	errGroup.Go(func() error {
		// expected to close the channel that is producing work for workers
		return workSubmitter(taskCtx)
	})

	return errGroup.Wait()
}
