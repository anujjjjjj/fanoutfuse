package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

func TestMetadataParserAttributionAndUsage(t *testing.T) {
	p := Parser{}
	now := time.Now()
	events := p.Parse([]byte(`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"SECRET_RESPONSE"},{"type":"tool_use","id":"t1","name":"Agent","input":{"prompt":"SECRET_PROMPT","subagent_type":"review","run_in_background":true}}],"usage":{"input_tokens":12,"output_tokens":4,"cache_read_input_tokens":3}}}`), "sid", "main", now)
	if len(events) != 2 || events[0].Kind != "intent" || !events[0].Background || events[1].TokensIn != 12 || events[1].TokensCache != 3 {
		t.Fatalf("%+v", events)
	}
	result := p.Parse([]byte(`{"message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"agentId: abc123 (resume this agent)"}]}}`), "sid", "main", now)
	if len(result) != 1 || result[0].Parent != "main" || result[0].Agent != "abc123" {
		t.Fatalf("attribution %+v", result)
	}
	duplicate := p.Parse([]byte(`{"message":{"id":"m1","usage":{"input_tokens":12,"output_tokens":4,"cache_read_input_tokens":3}}}`), "sid", "main", now)
	if len(duplicate) != 0 {
		t.Fatal("usage counted twice")
	}
	if got := fmt.Sprint(events); len(got) == 0 {
		t.Fatal("no metadata")
	}
}
func TestWatcherCreationPartialLineAndRotation(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "projects", "project", "session", "subagents")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan model.Event, 32)
	done := make(chan error, 1)
	w := Watcher{Root: root}
	go func() { done <- w.Watch(ctx, out) }()
	path := filepath.Join(sub, "agent-child.jsonl")
	if err := os.WriteFile(path, []byte(`{"message":{"id":"m1","usage":{"input_tokens":7}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(4 * time.Second)
	spawned := false
	for !spawned {
		select {
		case e := <-out:
			if e.Kind == "spawn" {
				spawned = true
				if e.Session != "session" || e.Agent != "child" {
					t.Fatal(e)
				}
			}
		case <-deadline:
			t.Fatal("missed create")
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.After(4 * time.Second)
	used := false
	for !used {
		select {
		case e := <-out:
			if e.Kind == "usage" {
				if e.TokensIn != 7 {
					t.Fatal(e)
				}
				used = true
			}
		case <-deadline:
			t.Fatal("partial line was lost")
		}
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
