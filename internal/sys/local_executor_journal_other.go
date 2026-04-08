//go:build !linux

package sys

import (
	"context"
	"fmt"
)

func journalRead(_ context.Context, unit string, limit int, filter string) ([]string, error) {
	return nil, fmt.Errorf("journal access not supported on this platform")
}

func journalWalk(_ context.Context, unit string, newestFirst bool, visit func(string) error) error {
	return fmt.Errorf("journal access not supported on this platform")
}
