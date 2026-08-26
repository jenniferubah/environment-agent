// Package util provides small helpers shared across in-repo OpenShift service providers.
package util

import (
	"context"

	"k8s.io/client-go/kubernetes"
)

// ServerVersion calls the Kubernetes discovery API and returns when the call
// completes or ctx is cancelled.
func ServerVersion(ctx context.Context, client kubernetes.Interface) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	type result struct {
		err error
	}
	ch := make(chan result, 1)
	go func() {
		_, err := client.Discovery().ServerVersion()
		ch <- result{err: err}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		return r.err
	}
}
