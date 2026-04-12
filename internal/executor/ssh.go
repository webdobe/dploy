package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
)

// SSHRunner executes steps against a remote host over SSH.
//
// Connection is established lazily on the first Run call and reused for
// the lifetime of the runner. Close tears it down.
//
// The host field is resolved against ~/.ssh/config, so aliases like
// "prod-web-1" work as long as they're defined there. Explicit parts
// of the field (user@, :port) override config values. Supported field
// shapes:
//
//   - "prod-web-1"                    (alias, resolved via ssh_config)
//   - "example.com"
//   - "user@example.com"
//   - "example.com:2222"
//   - "user@example.com:2222"
//
// Authentication tries, in order:
//
//  1. SSH agent (SSH_AUTH_SOCK)
//  2. IdentityFile from ssh_config, if set for this alias
//  3. Default key files in ~/.ssh (id_ed25519, id_rsa, id_ecdsa)
//
// Encrypted keys that require a passphrase are skipped — use the agent.
//
// Host key verification uses ~/.ssh/known_hosts and fails closed:
// unknown hosts produce an error, not a prompt. Users must ssh in
// manually once to populate known_hosts before dploy can connect.
type SSHRunner struct {
	host   string
	stream io.Writer

	once      sync.Once
	dialErr   error
	client    *ssh.Client
	agentConn net.Conn // non-nil when the agent was used; closed in Close()
}

// NewSSHRunner constructs an SSHRunner. The connection is deferred
// until the first Run call.
func NewSSHRunner(host string, stream io.Writer) *SSHRunner {
	return &SSHRunner{host: host, stream: stream}
}

// Run executes step.Command on the remote host after cd'ing into
// target.Path. Output is streamed live to the configured writer (when
// non-nil) and captured in StepResult.Output regardless.
func (r *SSHRunner) Run(ctx context.Context, target planner.TargetPlan, step planner.Step) operation.StepResult {
	start := time.Now()
	result := operation.StepResult{
		Index:   step.Index,
		Command: step.Command,
		Target:  target.Name,
	}

	if err := r.connect(); err != nil {
		result.Status = operation.StatusFailedExecution
		result.ExitCode = -1
		result.Error = fmt.Sprintf("ssh connect: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	session, err := r.client.NewSession()
	if err != nil {
		result.Status = operation.StatusFailedExecution
		result.ExitCode = -1
		result.Error = fmt.Sprintf("ssh session: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	defer session.Close()

	var buf bytes.Buffer
	var out io.Writer = &buf
	if r.stream != nil {
		out = io.MultiWriter(r.stream, &buf)
	}
	session.Stdout = out
	session.Stderr = out

	remoteCmd := buildRemoteCommand(target.Path, step.Command)

	// Run the command, watching ctx for cancellation.
	done := make(chan error, 1)
	go func() { done <- session.Run(remoteCmd) }()

	select {
	case err = <-done:
		// completed normally
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		<-done // wait for the goroutine to drain
		err = ctx.Err()
	}

	result.Output = buf.String()
	result.Duration = time.Since(start)

	if err != nil {
		result.Status = operation.StatusFailedExecution
		result.Error = err.Error()
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitStatus()
		} else {
			result.ExitCode = -1
		}
		return result
	}

	result.Status = operation.StatusSuccess
	return result
}

// Close closes the SSH connection and the agent connection, if any.
func (r *SSHRunner) Close() error {
	var firstErr error
	if r.client != nil {
		if err := r.client.Close(); err != nil {
			firstErr = err
		}
	}
	if r.agentConn != nil {
		if err := r.agentConn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// connect establishes the SSH client, at most once per runner.
func (r *SSHRunner) connect() error {
	r.once.Do(func() {
		r.client, r.agentConn, r.dialErr = r.dial()
	})
	return r.dialErr
}

// dial resolves the target, builds auth methods, and opens the SSH
// connection. Returns (client, agentConn, err). On error, both returned
// values are nil and any agent conn opened during the attempt is closed.
func (r *SSHRunner) dial() (*ssh.Client, net.Conn, error) {
	target := resolveSSHTarget(r.host, ssh_config.Get)

	var authMethods []ssh.AuthMethod
	agentConn, _ := dialAgent()
	if agentConn != nil {
		authMethods = append(authMethods, ssh.PublicKeysCallback(agent.NewClient(agentConn).Signers))
	}

	var extraKeys []string
	if target.identityFile != "" {
		extraKeys = append(extraKeys, target.identityFile)
	}
	if keySigners := loadKeyFileSigners(extraKeys...); len(keySigners) > 0 {
		authMethods = append(authMethods, ssh.PublicKeys(keySigners...))
	}

	if len(authMethods) == 0 {
		closeIfNotNil(agentConn)
		return nil, nil, errors.New("no ssh auth methods available (agent not reachable, no readable keys in ~/.ssh)")
	}

	hkcb, err := loadHostKeyCallback()
	if err != nil {
		closeIfNotNil(agentConn)
		return nil, nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            target.user,
		Auth:            authMethods,
		HostKeyCallback: hkcb,
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(target.host, target.port), cfg)
	if err != nil {
		closeIfNotNil(agentConn)
		return nil, nil, err
	}
	return client, agentConn, nil
}

// --- helpers ---

// buildRemoteCommand prepends `cd '<escaped path>' &&` to cmd. Single
// quotes in path are escaped with the POSIX `'\''` idiom.
func buildRemoteCommand(path, cmd string) string {
	escaped := replaceAllSingleQuote(path)
	return fmt.Sprintf("cd '%s' && %s", escaped, cmd)
}

func replaceAllSingleQuote(s string) string {
	// Inlined strings.ReplaceAll to keep buildRemoteCommand's import list short
	// and make the escaping rule visible next to the code that relies on it.
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\\', '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// dialAgent opens a connection to the ssh-agent socket if available.
// Returns (nil, nil) when SSH_AUTH_SOCK is unset.
func dialAgent() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil
	}
	return net.Dial("unix", sock)
}

// loadKeyFileSigners reads private keys from disk. extra paths are tried
// first (intended for ssh_config's IdentityFile), then the common defaults
// in ~/.ssh. Keys that fail to parse (including encrypted keys requiring
// a passphrase) are silently skipped.
func loadKeyFileSigners(extra ...string) []ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	paths := make([]string, 0, len(extra)+3)
	paths = append(paths, extra...)
	for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
		paths = append(paths, filepath.Join(home, ".ssh", name))
	}

	seen := make(map[string]bool, len(paths))
	var out []ssh.Signer
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			continue
		}
		out = append(out, signer)
	}
	return out
}

// loadHostKeyCallback verifies against ~/.ssh/known_hosts. Fails closed
// on missing file — we never silently accept unknown host keys.
func loadHostKeyCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w (ssh to the host manually once to accept its host key)", path, err)
	}
	return cb, nil
}

func closeIfNotNil(c io.Closer) {
	if c != nil {
		_ = c.Close()
	}
}
