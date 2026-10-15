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
