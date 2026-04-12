// Package provider defines the contracts for the pluggable pieces of dploy.
//
// Providers return data, not behavior. They must not:
//   - execute operations themselves
//   - bypass policy
//   - mutate scope
//
// Core behavior is fixed. Integrations are replaceable (see
// EXTENSIBILITY_MODEL.md).
package provider

import "context"

// SecretsProvider resolves a secret reference to its value.
// Implementations: env, dotenv, vault, 1password, aws-secrets-manager, ...
type SecretsProvider interface {
	Resolve(ctx context.Context, key string) (string, error)
}

// PolicyProvider loads a trusted policy from its source.
// Implementations: local file, signed file, remote endpoint, ...
type PolicyProvider interface {
	Load(ctx context.Context) (source string, body []byte, err error)
}

// ArtifactProvider resolves an artifact reference to something the
// executor can deploy (image tag, local path, tarball, etc.).
// Implementations: local file, OCI registry, S3/GCS, GitHub releases, ...
type ArtifactProvider interface {
	Resolve(ctx context.Context, ref string) (location string, err error)
}

// LogSink receives structured log entries for external forwarding.
// Implementations: file, stdout, HTTP endpoint, logging service, ...
type LogSink interface {
	Write(entry LogEntry) error
	Close() error
}

// LogEntry is one structured record sent to a LogSink.
type LogEntry struct {
	Operation   string
	Environment string
	Target      string
	Step        string
	Stream      string // "stdout" or "stderr"
	Line        string
}
