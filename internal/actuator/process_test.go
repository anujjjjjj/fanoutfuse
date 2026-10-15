package actuator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

func TestTmuxFindsSessionTTYAndSendsTwice(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nif [ \"$1\" = list-panes ]; then printf '%%7 /dev/pts/99\\n%%8 /dev/pts/100\\n'; else printf '%s\\n' \"$*\" >> \"$FF_TEST_LOG\"; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("FF_TEST_LOG", log)
	if err := StopBackground(context.Background(), model.Session{ID: "s", TTY: "/dev/pts/99"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "send-keys -t %7 C-x C-k\nsend-keys -t %7 C-x C-k\n" {
		t.Fatal(string(b))
	}
}
func TestSignalGroupStopsAndResumes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = SignalGroup(cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
	for _, test := range []struct {
		sig     syscall.Signal
		stopped bool
	}{{syscall.SIGSTOP, true}, {syscall.SIGCONT, false}} {
		if err := SignalGroup(cmd.Process.Pid, test.sig); err != nil {
			t.Fatal(err)
		}
		until := time.Now().Add(time.Second)
		matched := false
		for time.Now().Before(until) {
			b, err := exec.Command("ps", "-p", strconv.Itoa(cmd.Process.Pid), "-o", "stat=").Output()
			if err == nil && strings.Contains(string(b), "T") == test.stopped {
				matched = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !matched {
			t.Fatalf("signal %v did not change actual process state", test.sig)
		}
	}
	if err := SignalGroup(os.Getpid(), syscall.SIGSTOP); err == nil {
		t.Fatal("own group accepted")
	}
}
