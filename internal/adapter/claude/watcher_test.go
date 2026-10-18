package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
	"github.com/fsnotify/fsnotify"
)

func TestMetaDepthAndBackground(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "project", "session", "subagents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"agent-child.jsonl":     "",
		"agent-child.meta.json": `{"agentType":"general-purpose","description":"private text","toolUseId":"toolu_test","parentAgentId":"parent","spawnDepth":2,"requestShape":"background"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	w := &Watcher{Root: root, tails: map[string]*tail{}, seen: map[string]bool{}, watched: map[string]bool{}}
	found := false
	w.scan(watcher, func(e model.Event) {
		if e.Kind != "attribute" {
			return
		}
		found = true
		if e.Parent != "parent" || e.Type != "general-purpose" || e.SpawnDepth == nil || *e.SpawnDepth != 2 || !e.Background {
			t.Fatalf("unexpected metadata: %+v", e)
		}
	})
	if !found {
		t.Fatal("no metadata emitted")
	}
}

// Workflow agents write transcripts under <session>/subagents/workflows/<run-id>/
// (observed with Claude Code 2.1.300). They must count as spawns of the session,
// not as new main sessions.
func TestWorkflowTranscriptsCountAsSpawns(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "project", "session", "subagents", "workflows", "wf_run-1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"agent-w1.jsonl":     "",
		"agent-w1.meta.json": `{"agentType":"workflow-subagent","description":"s1","workflowPhase":"Sleep","spawnDepth":1,"requestShape":"foreground"}`,
		"agent-w2.jsonl":     "",
		"journal.jsonl":      `{"type":"started","agentId":"w1"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	w := &Watcher{Root: root, tails: map[string]*tail{}, seen: map[string]bool{}, watched: map[string]bool{}}
	spawns := map[string]string{}
	w.scan(watcher, func(e model.Event) {
		if e.Kind == "spawn" {
			spawns[e.Agent] = e.Session
		}
		if e.Kind == "attribute" && e.Agent == "w1" && e.Type != "workflow-subagent" {
			t.Fatalf("workflow agent type lost: %+v", e)
		}
	})
	if len(spawns) != 2 || spawns["w1"] != "session" || spawns["w2"] != "session" {
		t.Fatalf("workflow spawns = %v, want w1 and w2 in session", spawns)
	}
}
