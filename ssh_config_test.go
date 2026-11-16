package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSSHConfig(t *testing.T) {
	dir := t.TempDir()
	includeDir := filepath.Join(dir, "config.d")
	if err := os.Mkdir(includeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mainConfig := `# primary config
Host dev staging
  HostName dev.example.com
  User alice
  Port 2222

Host *
  ServerAliveInterval 30

Host production
  HostName 10.0.0.8

Include config.d/*
`
	includeConfig := `Host database
  HostName db.internal
  User postgres
  Port 5433

Host dev
  User ignored-second-value
`
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(mainConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(includeDir, "extra.conf"), []byte(includeConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	hosts, err := ParseSSHConfig(filepath.Join(dir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 4 {
		t.Fatalf("got %d hosts, want 4: %#v", len(hosts), hosts)
	}

	want := []SSHHost{
		{Name: "dev", HostName: "dev.example.com", User: "alice", Port: 2222},
		{Name: "staging", HostName: "dev.example.com", User: "alice", Port: 2222},
		{Name: "production", HostName: "10.0.0.8", Port: 22},
		{Name: "database", HostName: "db.internal", User: "postgres", Port: 5433},
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Errorf("host %d = %#v, want %#v", i, hosts[i], want[i])
		}
	}
}

func TestSplitSSHConfigLine(t *testing.T) {
	fields := splitSSHConfigLine(`Host "my server" # comment`)
	want := []string{"Host", "my server"}
	if len(fields) != len(want) {
		t.Fatalf("fields = %#v", fields)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("field %d = %q, want %q", i, fields[i], want[i])
		}
	}
}
