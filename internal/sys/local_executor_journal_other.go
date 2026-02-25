//go:build !linux

package sys

import "fmt"

func journalRead(unit string, limit int, filter string) ([]string, error) {
	return nil, fmt.Errorf("journal access not supported on this platform")
}
