package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitHostField(t *testing.T) {
	tests := []struct {
		in       string
		wantUser string
		wantHost string
		wantPort string
	}{
		{"example.com", "", "example.com", ""},
		{"bob@example.com", "bob", "example.com", ""},
		{"example.com:2222", "", "example.com", "2222"},
		{"bob@example.com:2222", "bob", "example.com", "2222"},
		{"192.168.1.10", "", "192.168.1.10", ""},
		{"deploy@10.0.0.5:2022", "deploy", "10.0.0.5", "2022"},
		{"prod-web-1", "", "prod-web-1", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			gotUser, gotHost, gotPort := splitHostField(tc.in)
			if gotUser != tc.wantUser || gotHost != tc.wantHost || gotPort != tc.wantPort {
				t.Errorf("splitHostField(%q) = (%q, %q, %q); want (%q, %q, %q)",
					tc.in, gotUser, gotHost, gotPort, tc.wantUser, tc.wantHost, tc.wantPort)
			}
		})
	}
}

func TestResolveSSHTarget(t *testing.T) {
	t.Setenv("USER", "alice")

	// Simulates an ssh_config with one alias defined. Aliases not in
	// this map behave as if no config file existed.
	config := map[string]map[string]string{
		"prod-web-1": {
			"HostName":     "web1.prod.example.com",
			"User":         "deploy",
			"Port":         "2222",
			"IdentityFile": "/absolute/path/to/prod_key",
		},
	}
	get := func(alias, key string) string { return config[alias][key] }

	tests := []struct {
		name             string
		field            string
		wantUser         string
		wantHost         string
		wantPort         string
		wantIdentityFile string
	}{
		{
			name:             "alias fully resolved via config",
			field:            "prod-web-1",
			wantUser:         "deploy",
			wantHost:         "web1.prod.example.com",
			wantPort:         "2222",
			wantIdentityFile: "/absolute/path/to/prod_key",
		},
		{
			name:             "explicit user overrides config user",
			field:            "bob@prod-web-1",
			wantUser:         "bob",
			wantHost:         "web1.prod.example.com",
			wantPort:         "2222",
			wantIdentityFile: "/absolute/path/to/prod_key",
		},
		{
			name:             "explicit port overrides config port",
			field:            "prod-web-1:9999",
			wantUser:         "deploy",
			wantHost:         "web1.prod.example.com",
			wantPort:         "9999",
			wantIdentityFile: "/absolute/path/to/prod_key",
		},
		{
			name:             "both overrides take effect",
			field:            "alice@prod-web-1:9999",
			wantUser:         "alice",
			wantHost:         "web1.prod.example.com",
			wantPort:         "9999",
			wantIdentityFile: "/absolute/path/to/prod_key",
		},
		{
			name:             "alias not in config falls through to defaults",
			field:            "unknown.example.com",
			wantUser:         "alice",
			wantHost:         "unknown.example.com",
			wantPort:         "22",
			wantIdentityFile: "",
		},
		{
			name:             "explicit user@ with unknown alias",
			field:            "bob@unknown.example.com",
			wantUser:         "bob",
			wantHost:         "unknown.example.com",
			wantPort:         "22",
			wantIdentityFile: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSSHTarget(tc.field, get)
			if got.user != tc.wantUser || got.host != tc.wantHost ||
				got.port != tc.wantPort || got.identityFile != tc.wantIdentityFile {
				t.Errorf("resolveSSHTarget(%q) = %+v; want user=%q host=%q port=%q id=%q",
					tc.field, got, tc.wantUser, tc.wantHost, tc.wantPort, tc.wantIdentityFile)
			}
		})
	}
}

func TestResolveSSHTarget_ExpandsTildeInIdentityFile(t *testing.T) {
	t.Setenv("USER", "alice")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}

	get := func(alias, key string) string {
		if alias == "tilde-host" && key == "IdentityFile" {
			return "~/.ssh/custom_key"
		}
		return ""
	}

	got := resolveSSHTarget("tilde-host", get)
	want := filepath.Join(home, ".ssh", "custom_key")
	if got.identityFile != want {
		t.Errorf("identityFile = %q; want %q", got.identityFile, want)
	}
}

func TestBuildRemoteCommand(t *testing.T) {
	tests := []struct {
		path string
		cmd  string
		want string
	}{
		{"/var/www/app", "git pull", `cd '/var/www/app' && git pull`},
		{"/tmp", "echo hi", `cd '/tmp' && echo hi`},
		// Paths containing a single quote must be escaped so the wrapping
		// single-quoted string stays well-formed.
		{"/var/www/bob's app", "ls", `cd '/var/www/bob'\''s app' && ls`},
		{"/a path/with spaces", "true", `cd '/a path/with spaces' && true`},
	}
	for _, tc := range tests {
		got := buildRemoteCommand(tc.path, tc.cmd)
		if got != tc.want {
			t.Errorf("buildRemoteCommand(%q, %q) = %q; want %q", tc.path, tc.cmd, got, tc.want)
		}
	}
}

// TestNewSSHRunner_DoesNotDial ensures constructing a runner doesn't
// perform any I/O. This matters because the Sequential executor builds
// runners eagerly; dialing at construction time would cost us even for
// targets we never reach after a failure.
func TestNewSSHRunner_DoesNotDial(t *testing.T) {
	r := NewSSHRunner("nonexistent.invalid", os.Stderr)
	if r == nil {
		t.Fatal("NewSSHRunner returned nil")
	}
	if r.client != nil {
		t.Error("client should be nil before first Run")
	}
}
