package executor

import (
	"os"
	"path/filepath"
	"strings"
)

// sshTarget is a fully-resolved SSH connection target, after applying
// both the user's dploy.yml host field and ~/.ssh/config.
type sshTarget struct {
	user         string
	host         string
	port         string
	identityFile string // "" when unset
}

// sshConfigGetter reads one key for one alias from an ssh_config source.
// Passed in as a function so tests can inject fake config data without
// writing real files to disk. In production, ssh_config.Get is used.
type sshConfigGetter func(alias, key string) string

// resolveSSHTarget builds a connection target from a dploy.yml host field,
// applying ssh_config as a layer between the literal field and defaults.
//
// Precedence, highest first:
//
//  1. explicit parts of the field (user@, :port)
//  2. ssh_config for the alias (HostName, User, Port, IdentityFile)
//  3. built-in defaults ($USER for user, "22" for port, alias itself for host)
//
// The alias used for ssh_config lookup is the host portion of the field,
// stripped of any user@ prefix or :port suffix — matching what `ssh(1)`
// itself does.
func resolveSSHTarget(hostField string, get sshConfigGetter) sshTarget {
	literalUser, alias, literalPort := splitHostField(hostField)

	t := sshTarget{user: literalUser, port: literalPort}

	// HostName: ssh_config gets to rename the host.
	t.host = get(alias, "HostName")
	if t.host == "" {
		t.host = alias
	}

	// User: explicit overrides config overrides $USER.
	if t.user == "" {
		t.user = get(alias, "User")
	}
	if t.user == "" {
		t.user = os.Getenv("USER")
	}

	// Port: explicit overrides config overrides 22.
	if t.port == "" {
		t.port = get(alias, "Port")
	}
	if t.port == "" {
		t.port = "22"
	}

	// IdentityFile: only from config. Expand a leading ~/.
	t.identityFile = expandTilde(get(alias, "IdentityFile"))

	return t
}

// splitHostField splits "[user@]host[:port]" into its parts without
// applying defaults. Empty strings indicate "not specified in the field".
func splitHostField(s string) (user, host, port string) {
	if at := strings.Index(s, "@"); at >= 0 {
		user = s[:at]
		s = s[at+1:]
	}
	if colon := strings.Index(s, ":"); colon >= 0 {
		host = s[:colon]
		port = s[colon+1:]
	} else {
		host = s
	}
	return
}

// expandTilde replaces a leading "~/" with the user's home directory.
// Anything else (including absolute paths and unset strings) is returned unchanged.
func expandTilde(p string) string {
	if p == "" || !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}
