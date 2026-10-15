package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/actuator"
	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/ctl"
	"github.com/anujjjjjj/fanoutfuse/internal/store"
	"github.com/anujjjjjj/fanoutfuse/internal/tui"
)

func TestLiveFilesToFuseTmuxSQLiteAndControl(t *testing.T) {
	root := t.TempDir()
	c := config.Default()
	c.Home = filepath.Join(root, "state")
	c.ClaudeRoot = filepath.Join(root, "profile")
	c.General.Notify = false
	c.General.Escalate = "40ms"
	c.Limits["spawn_burst"] = config.Limit{Trip: 3}
	sub := filepath.Join(c.ClaudeRoot, "projects", "p", "session-live", "subagents")
	registry := filepath.Join(c.ClaudeRoot, "sessions")
	bin := filepath.Join(root, "bin")
	for _, dir := range []string{sub, registry, bin} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	data := fmt.Sprintf(`{"sessionId":"session-live","pid":%d,"tty":"/dev/pts/123"}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(registry, "test.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "tmux.log")
	t.Setenv("FF_TEST_LOG", log)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nif [ \"$1\" = list-panes ]; then printf '%%4 /dev/pts/123\\n'; else printf '%s\\n' \"$*\" >> \"$FF_TEST_LOG\"; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, func() bool { _, err := ctl.Client(c.Home, "status"); return err == nil })
	info, err := os.Stat(filepath.Join(c.Home, "ctl.sock"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("socket permissions", err)
	}
	for i := 0; i < 3; i++ {
		line := fmt.Sprintf("{\"message\":{\"id\":\"m%d\",\"content\":[{\"type\":\"text\",\"text\":\"SECRET_RESPONSE\"}],\"usage\":{\"input_tokens\":7}}}\n", i)
		if err = os.WriteFile(filepath.Join(sub, fmt.Sprintf("agent-%d.jsonl", i)), []byte(line), 0600); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(c.Home, "fuse", "session-live")); return err == nil })
	waitFor(t, func() bool { b, _ := os.ReadFile(log); return strings.Count(string(b), "send-keys -t %4 C-x C-k") == 2 })
	var guard bytes.Buffer
	if err = actuator.Guard(c.Home, strings.NewReader(`{"session_id":"session-live","tool_name":"Agent"}`), &guard); err != nil || !strings.Contains(guard.String(), "spawn_burst") {
		t.Fatal(guard.String(), err)
	}
	s, err := store.Open(c.Home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	waitFor(t, func() bool { in, err := s.Get(""); return err == nil && in.TokensIn == 21 })
	in, err := s.Get("")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(in)
	if bytes.Contains(encoded, []byte("SECRET")) {
		t.Fatal("transcript text leaked")
	}
	var pm bytes.Buffer
	if err = tui.Render(&pm, in, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pm.String(), "stop_background") {
		t.Fatal(pm.String())
	}
	if _, err = ctl.Client(c.Home, "stop"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(c.Home, "fuse", "ALL")); err != nil {
		t.Fatal("manual stop missing global fuse")
	}
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for live pipeline")
}
