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

	"github.com/anujjjjjj/fanoutfuse/internal/config"
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

func TestPauseGroupHolds(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = SignalGroup(cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
	if err := PauseGroup(context.Background(), cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	stopped, err := processStopped(context.Background(), cmd.Process.Pid)
	if err != nil || !stopped {
		t.Fatalf("stopped=%t: %v", stopped, err)
	}
}

func TestPauseAutoContinueAdvancesLadder(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = SignalGroup(cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = SignalGroup(cmd.Process.Pid, syscall.SIGCONT)
			}
		}
	}()
	defer func() { close(done); <-exited }()
	c := config.Default()
	c.Home = t.TempDir()
	c.General.Ladder = []string{"pause", "contain"}
	at := time.Now()
	in := &model.Incident{After: 1}
	l := &Ladder{Config: c, Session: model.Session{ID: "test", PID: cmd.Process.Pid}, Incident: in, Waiting: true, Deadline: at}
	l.Advance(context.Background(), at)
	if len(in.Actions) != 3 || in.Actions[1].Step != "pause" || in.Actions[1].OK || !strings.Contains(in.Actions[1].Error, "pause did not hold: process was auto-continued (tmux resumes stopped pane processes); use stop_background or kill") || in.Actions[2].Step != "contain" || !in.Actions[2].OK {
		t.Fatalf("unexpected actions: %+v", in.Actions)
	}
}
