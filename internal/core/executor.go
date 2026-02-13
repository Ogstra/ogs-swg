package core

import (
	"context"
	"os"
)

// SystemExecutor defines the interface for system-level operations.
// It abstracts local and remote (SSH) execution.
type SystemExecutor interface {
	// Service Management
	RestartService(ctx context.Context, name string) error
	StartService(ctx context.Context, name string) error
	StopService(ctx context.Context, name string) error
	IsServiceActive(ctx context.Context, name string) (bool, error)

	// File Management (Atomic Operations where possible)
	// WriteConfig writes content to the specified path.
	// Implementation should ensure atomic writes or safe replacements.
	WriteConfig(ctx context.Context, path string, content []byte, fileMode os.FileMode) error
	// ReadConfig reads content from the specified path.
	ReadConfig(ctx context.Context, path string) ([]byte, error)

	// Sysctl Management
	// ApplySysctl sets a kernel parameter. Implementation MUST enforce a whitelist.
	ApplySysctl(ctx context.Context, key, value string) error
	// GetSysctl retrieves a kernel parameter. Implementation MUST enforce a whitelist.
	GetSysctl(ctx context.Context, key string) (string, error)

	// Log Management
	// ReadJournal reads the last N lines from a systemd journal unit.
	ReadJournal(ctx context.Context, unit string, limit int) ([]string, error)
	// SearchJournal searches for a query string in the journal.
	SearchJournal(ctx context.Context, unit, query string, limit int) ([]string, error)

	// SyncWireGuard applies the WireGuard configuration to the interface.
	SyncWireGuard(ctx context.Context, interfaceName string, configContent []byte) error

	// ValidateSingboxConfig validates the sing-box configuration content.
	ValidateSingboxConfig(ctx context.Context, content []byte) error

	// Lifecycle
	// CheckConnectivity verifies if the underlying system (e.g. SSH connection) is reachable.
	CheckConnectivity(ctx context.Context) error
	// Close releases any resources held by the executor.
	Close() error
}
