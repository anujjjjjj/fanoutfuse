package actuator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

func SignalGroup(pid int, sig syscall.Signal) error {
	if pid <= 1 {
		return fmt.Errorf("refusing unsafe pid %d", pid)
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return err
	}
	own, err := syscall.Getpgid(0)
	if err != nil {
		return err
	}
	if pgid <= 1 || pgid == own {
		return fmt.Errorf("refusing to signal own or system process group %d", pgid)
	}
	return syscall.Kill(-pgid, sig)
}

// PauseGroup verifies that the stop survives automatic pane process resumes.
func PauseGroup(ctx context.Context, pid int) error {
	if err := SignalGroup(pid, syscall.SIGSTOP); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(300 * time.Millisecond):
	}
	stopped, err := processStopped(ctx, pid)
	if err != nil {
		return fmt.Errorf("verify pause: %w", err)
	}
	if !stopped {
		return fmt.Errorf("pause did not hold: process was auto-continued (tmux resumes stopped pane processes); use stop_background or kill")
	}
	return nil
}

func processStopped(ctx context.Context, pid int) (bool, error) {
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return false, err
		}
		// The command name may contain spaces and parentheses.
		end := strings.LastIndexByte(string(b), ')')
		if end < 0 || end+2 >= len(b) {
			return false, fmt.Errorf("invalid process stat")
		}
		return b[end+2] == 'T', nil
	}
	b, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	return strings.HasPrefix(strings.TrimSpace(string(b)), "T"), err
}

func command(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
func StopBackground(ctx context.Context, s model.Session) error {
	return stopBackground(ctx, s, nil)
}
func stopBackground(ctx context.Context, s model.Session, record func(string, error)) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if s.Background {
		return command(ctx, "claude", "stop", s.ID)
	}
	tty := s.TTY
	if tty == "" && s.PID > 0 {
		b, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(s.PID), "-o", "tty=").Output()
		if err == nil {
			tty = strings.TrimSpace(string(b))
		}
	}
	if tty == "" || tty == "?" || tty == "??" {
		return fmt.Errorf("stop_background skipped: no session tty")
	}
	b, err := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", "#{pane_id} #{pane_tty}").Output()
	if err != nil {
		return fmt.Errorf("tmux panes: %w", err)
	}
	pane := ""
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && (f[1] == tty || strings.TrimPrefix(f[1], "/dev/") == strings.TrimPrefix(tty, "/dev/")) {
			pane = f[0]
			break
		}
	}
	if pane == "" {
		return fmt.Errorf("stop_background skipped: tty %s not in tmux", tty)
	}
	for i := 0; i < 2; i++ {
		err := command(ctx, "tmux", "send-keys", "-t", pane, "C-x", "C-k")
		if record != nil {
			record(fmt.Sprintf("tmux_chord_%d", i+1), err)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func Notify(ctx context.Context, message string, bell bool) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if bell {
		fmt.Print("\a")
	}
	if _, err := exec.LookPath("notify-send"); err == nil {
		_ = command(ctx, "notify-send", "FanoutFuse", message)
		return
	}
	if _, err := exec.LookPath("osascript"); err == nil {
		_ = command(ctx, "osascript", "-e", "display notification "+strconv.Quote(message)+" with title \"FanoutFuse\"")
	}
}

// ExecutableCommand produces a shell-safe absolute hook command.
func ExecutableCommand(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return "'" + strings.ReplaceAll(p, "'", "'\"'\"'") + "' guard", nil
}
func KillGroup(ctx context.Context, pid int, record func(string, error)) error {
	for i, s := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL} {
		err := SignalGroup(pid, s)
		record(strings.ToLower(s.String()), err)
		if err != nil {
			return err
		}
		if i == 2 {
			return nil
		}
		wait := 3 * time.Second
		if i == 1 {
			wait = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		if err = syscall.Kill(pid, 0); err == syscall.ESRCH {
			return nil
		}
	}
	return nil
}
