// Command fanoutfuse monitors and contains local subagent fan-out.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/anujjjjjj/fanoutfuse/internal/actuator"
	"github.com/anujjjjjj/fanoutfuse/internal/adapter/claude"
	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/ctl"
	"github.com/anujjjjjj/fanoutfuse/internal/daemon"
	"github.com/anujjjjjj/fanoutfuse/internal/replay"
	"github.com/anujjjjjj/fanoutfuse/internal/store"
	"github.com/anujjjjjj/fanoutfuse/internal/tui"
)

var version = "1.0.0"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "fanoutfuse:", err)
		os.Exit(1)
	}
}
func run(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(out, "FanoutFuse v"+version+"\nUsage: fanoutfuse version|run|guard|status|stop|pause <session>|resume <session>|pm [--md] [incident]|doctor|install|uninstall|replay <fixture.yaml>|config --explain|purge")
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(out, version)
		return nil
	}
	// Keep the enforcement path free of config, database, socket, and process discovery work.
	if args[0] == "guard" {
		return actuator.Guard(config.Home(), in, out)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	c, err := config.Load(cwd)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch args[0] {
	case "run":
		return daemon.Run(ctx, c)
	case "config":
		if len(args) != 2 || args[1] != "--explain" {
			return fmt.Errorf("usage: fanoutfuse config --explain")
		}
		fmt.Fprint(out, c.Explain())
		return nil
	case "install", "uninstall":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err = actuator.Install(c.ClaudeRoot, exe, args[0] == "uninstall"); err != nil {
			return err
		}
		fmt.Fprintln(out, args[0]+": synchronous guard settings updated")
		return nil
	case "doctor":
		return doctor(out, c)
	case "status", "stop", "pause", "resume":
		if (args[0] == "pause" || args[0] == "resume") && len(args) != 2 {
			return fmt.Errorf("%s requires a session id or pid", args[0])
		}
		if args[0] == "pause" {
			live, err := claude.Resolve(c.ClaudeRoot, args[1])
			if err != nil {
				return err
			}
			pgid, err := syscall.Getpgid(live.PID)
			if err != nil {
				return err
			}
			own, err := syscall.Getpgid(0)
			if err != nil {
				return err
			}
			if pgid == own {
				return fmt.Errorf("refusing to pause own process group %d", pgid)
			}
		}
		if msg, err := ctl.Client(c.Home, args[0], args[1:]...); err == nil {
			fmt.Fprintln(out, strings.TrimSpace(msg))
			return nil
		}
		if args[0] == "status" {
			fmt.Fprintln(out, "FanoutFuse daemon is not running")
			return nil
		}
		// Manual containment and process control remain available if the daemon is down.
		s, err := store.Open(c.Home)
		if err != nil {
			return err
		}
		defer s.Close()
		d := daemon.New(c, s, false)
		msg, err := d.Control(ctx, args[0], args[1:])
		if err == nil {
			fmt.Fprintln(out, msg)
		}
		return err
	case "replay":
		if len(args) != 2 {
			return fmt.Errorf("usage: fanoutfuse replay <fixture.yaml>")
		}
		s, err := store.Open(c.Home)
		if err != nil {
			return err
		}
		defer s.Close()
		_, err = replay.Run(ctx, args[1], c, s, out)
		return err
	case "pm":
		md := false
		id := ""
		for _, a := range args[1:] {
			if a == "--md" {
				md = true
			} else if id == "" {
				id = a
			} else {
				return fmt.Errorf("usage: fanoutfuse pm [--md] [incident]")
			}
		}
		s, err := store.Open(c.Home)
		if err != nil {
			return err
		}
		defer s.Close()
		incident, err := s.Get(id)
		if err != nil {
			return err
		}
		return tui.Render(out, incident, md)
	case "purge":
		if _, err := ctl.Client(c.Home, "status"); err == nil {
			return fmt.Errorf("stop the daemon process before purge")
		}
		abs, err := filepath.Abs(c.Home)
		if err != nil {
			return err
		}
		homeAbs, _ := filepath.Abs(os.Getenv("HOME"))
		if abs == "/" || abs == homeAbs || abs == cwd {
			return fmt.Errorf("refusing to purge unsafe home %s", abs)
		}
		if err = os.RemoveAll(c.Home); err != nil {
			return err
		}
		fmt.Fprintln(out, "FanoutFuse metadata purged")
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func doctor(out io.Writer, c config.Config) error {
	fmt.Fprintf(out, "FanoutFuse %s\nclaude root: %s\nCLI version: %s\n", version, c.ClaudeRoot, claude.Version(context.Background()))
	ss, warnings := claude.Sessions(c.ClaudeRoot)
	fmt.Fprintf(out, "active sessions: %d registered, %d including process fallback\n", len(ss), len(claude.Live(c.ClaudeRoot)))
	for _, s := range ss {
		fmt.Fprintf(out, " session %s pid=%d tty=%s\n", s.ID, s.PID, s.TTY)
	}
	installed, sync, err := actuator.HookHealth(c.ClaudeRoot)
	if err != nil {
		warnings = append(warnings, "settings: "+err.Error())
	}
	fmt.Fprintf(out, "hook installed: %t · synchronous (not async): %t\n", installed, sync)
	if !installed || !sync {
		warnings = append(warnings, "guard protection unhealthy: install a synchronous PreToolUse hook")
	}
	_, err = exec.LookPath("tmux")
	fmt.Fprintf(out, "tmux present: %t\n", err == nil)
	if err != nil {
		warnings = append(warnings, "tmux unavailable: graceful background stop will be skipped")
	}
	projects, _ := filepath.Glob(filepath.Join(c.ClaudeRoot, "projects", "*"))
	fmt.Fprintf(out, "project directories: %d\n", len(projects))
	metas, _ := filepath.Glob(filepath.Join(c.ClaudeRoot, "projects", "*", "*", "subagents", "*.meta.json"))
	if len(metas) == 0 {
		warnings = append(warnings, "undocumented agent meta.json files missing; parent attribution may be unknown")
	}
	warnings = append(warnings, "tmux resumes stopped pane processes automatically; pause may not hold; use stop_background or kill", "sessions/*.json and agent meta.json fields are undocumented; verify schema with your installed CLI", "live dispatch timing, tmux chord handling, and long SIGSTOP recovery require a live session smoke test")
	for _, key := range []string{"CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH", "CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS", "CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION"} {
		fmt.Fprintf(out, "%s=%s\n", key, os.Getenv(key))
	}
	fmt.Fprintln(out, "Effective limits:")
	fmt.Fprint(out, c.Explain())
	for _, w := range warnings {
		fmt.Fprintln(out, "WARNING:", w)
	}
	return nil
}
