package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeForwardSpec(t *testing.T) {
	spec, err := normalizeForwardSpec(ForwardSpec{
		Host:       " dev ",
		Kind:       "LOCAL",
		LocalPort:  8080,
		RemoteHost: "127.0.0.1",
		RemotePort: 5432,
		AutoStart:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Host != "dev" || spec.Kind != ForwardLocal || spec.BindAddress != "127.0.0.1" {
		t.Fatalf("unexpected normalized spec: %#v", spec)
	}
}

func TestBuildSSHArgs(t *testing.T) {
	args := buildSSHArgs(ForwardSpec{
		Host:        "dev",
		Kind:        ForwardLocal,
		BindAddress: "127.0.0.1",
		LocalPort:   8080,
		RemoteHost:  "db.internal",
		RemotePort:  5432,
	})
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"BatchMode=yes",
		"-L 127.0.0.1:8080:db.internal:5432",
		"-- dev",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("args %q does not contain %q", joined, expected)
		}
	}
}

func TestForwardManagerPersistsConfiguration(t *testing.T) {
	store := filepath.Join(t.TempDir(), "forwards.json")
	manager, err := NewForwardManager(store, "ssh")
	if err != nil {
		t.Fatal(err)
	}
	view, err := manager.Add(ForwardInput{
		Host:        "dev",
		Kind:        ForwardDynamic,
		BindAddress: "127.0.0.1",
		LocalPort:   1080,
		AutoStart:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewForwardManager(store, "ssh")
	if err != nil {
		t.Fatal(err)
	}
	forwards := reloaded.ListForHost("dev")
	if len(forwards) != 1 {
		t.Fatalf("got %d forwards, want 1", len(forwards))
	}
	if forwards[0].ID != view.ID || forwards[0].LocalPort != 1080 {
		t.Fatalf("unexpected reloaded forward: %#v", forwards[0])
	}

	second, err := manager.Add(ForwardInput{
		Host:        "dev",
		Kind:        ForwardLocal,
		BindAddress: "127.0.0.1",
		LocalPort:   8080,
		RemoteHost:  "127.0.0.1",
		RemotePort:  80,
	})
	if err != nil {
		t.Fatalf("overwrite existing store: %v", err)
	}
	if second.ID == view.ID {
		t.Fatal("expected a new forward ID")
	}
}

func TestForwardManagerProcessLifecycle(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses ping.exe as a harmless long-running child process")
	}
	ping, err := exec.LookPath("ping.exe")
	if err != nil {
		t.Skip("ping.exe is unavailable")
	}

	store := filepath.Join(t.TempDir(), "forwards.json")
	manager, err := NewForwardManager(store, "ssh")
	if err != nil {
		t.Fatal(err)
	}
	manager.commandFactory = func(ForwardSpec) (*exec.Cmd, error) {
		return exec.Command(ping, "-n", "20", "127.0.0.1"), nil
	}

	view, err := manager.Add(ForwardInput{
		Host:        "dev",
		Kind:        ForwardDynamic,
		BindAddress: "127.0.0.1",
		LocalPort:   1080,
	})
	if err != nil {
		t.Fatal(err)
	}

	running, err := manager.Start(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !running.Running || running.PID == 0 {
		t.Fatalf("expected a running process: %#v", running)
	}

	stopped, err := manager.Stop(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Running || stopped.PID != 0 {
		t.Fatalf("expected a stopped process: %#v", stopped)
	}
}
