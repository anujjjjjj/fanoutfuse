package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
	"github.com/fsnotify/fsnotify"
)

type tail struct {
	Offset int64
	Parser Parser
}
type Watcher struct {
	Root    string
	tails   map[string]*tail
	seen    map[string]bool
	watched map[string]bool
}

func (w *Watcher) Watch(ctx context.Context, out chan<- model.Event) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	w.tails = map[string]*tail{}
	w.seen = map[string]bool{}
	w.watched = map[string]bool{}
	emit := func(e model.Event) {
		select {
		case out <- e:
		case <-ctx.Done():
		}
	}
	scan := func() { w.scan(watcher, emit) }
	scan()
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
			scan()
		case _, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			scan()
		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			scan()
		}
	}
}
func (w *Watcher) scan(watcher *fsnotify.Watcher, emit func(model.Event)) {
	root := filepath.Join(w.Root, "projects")
	now := time.Now()
	active := map[string]bool{}
	for _, s := range LiveRegistry(w.Root) {
		active[s.ID] = true
	}
	add := func(p string) {
		if !w.watched[p] {
			if watcher.Add(p) == nil {
				w.watched[p] = true
			}
		}
	}
	add(w.Root)
	add(root)
	add(filepath.Join(w.Root, "sessions"))
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			depth := len(strings.Split(rel, string(filepath.Separator)))
			if rel == "." || depth == 1 {
				add(path)
				return nil
			}
			// Session directories are watched only while registry-active or recently modified.
			if depth == 2 {
				info, err := d.Info()
				if err != nil {
					return filepath.SkipDir
				}
				if !active[d.Name()] && now.Sub(info.ModTime()) > 10*time.Minute {
					return filepath.SkipDir
				}
			}
			add(path)
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sid, aid, ok := identity(path)
		if !ok {
			return nil
		}
		if !active[sid] && now.Sub(info.ModTime()) > 10*time.Minute {
			return nil
		}
		if aid != "main" && !w.seen[path] {
			w.seen[path] = true
			emit(model.Event{Kind: "spawn", Session: sid, Agent: aid, At: info.ModTime()})
		}
		if aid != "main" {
			metaPath := strings.TrimSuffix(path, ".jsonl") + ".meta.json"
			if b, err := os.ReadFile(metaPath); err == nil {
				var meta struct {
					Parent      string `json:"parentAgentId"`
					ParentSnake string `json:"parent_agent_id"`
					ParentID    string `json:"parentId"`
					Type        string `json:"subagent_type"`
					AgentType   string `json:"agentType"`
					Model       string `json:"model"`
				}
				if json.Unmarshal(b, &meta) == nil {
					parent := meta.Parent
					if parent == "" {
						parent = meta.ParentSnake
					}
					if parent == "" {
						parent = meta.ParentID
					}
					if meta.Type == "" {
						meta.Type = meta.AgentType
					}
					emit(model.Event{Kind: "attribute", Session: sid, Agent: aid, Parent: parent, Type: meta.Type, Model: meta.Model, At: now})
				}
			}
		}
		w.read(path, sid, aid, now, emit)
		return nil
	})
}
func LiveRegistry(root string) []model.Session { ss, _ := Sessions(root); return ss }
func identity(path string) (sid, aid string, ok bool) {
	if filepath.Base(filepath.Dir(path)) == "subagents" && strings.HasPrefix(filepath.Base(path), "agent-") {
		sid = filepath.Base(filepath.Dir(filepath.Dir(path)))
		aid = strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "agent-"), ".jsonl")
		return sid, aid, true
	}
	if strings.HasSuffix(path, ".jsonl") {
		return strings.TrimSuffix(filepath.Base(path), ".jsonl"), "main", true
	}
	return "", "", false
}
func (w *Watcher) read(path, sid, aid string, now time.Time, emit func(model.Event)) {
	state := w.tails[path]
	if state == nil {
		state = &tail{}
		w.tails[path] = state
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	if info.Size() < state.Offset {
		state.Offset = 0
		state.Parser = Parser{}
	}
	if _, err = f.Seek(state.Offset, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		state.Offset += int64(len(line))
		for _, e := range state.Parser.Parse(line, sid, aid, now) {
			emit(e)
		}
	}
}
