package actuator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SpawnMatcher covers every tool that starts subagents: Agent (alias Task) and
// Workflow, which also carries ultracode runs. Workflow agents do not pass
// through PreToolUse:Agent, so the Workflow launch itself must be gated.
const SpawnMatcher = "^(Task|Agent|Workflow)$"

func owned(h map[string]any) bool {
	cmd, _ := h["command"].(string)
	exe, _ := os.Executable()
	current, _ := ExecutableCommand(exe)
	return h["type"] == "command" && strings.HasSuffix(cmd, " guard") && (strings.Contains(cmd, "fanoutfuse") || cmd == current)
}
func entries(settings map[string]any) []any {
	hooks, _ := settings["hooks"].(map[string]any)
	v, _ := hooks["PreToolUse"].([]any)
	return v
}
func HookHealth(root string) (bool, bool, error) {
	b, err := os.ReadFile(filepath.Join(root, "settings.json"))
	if os.IsNotExist(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	var s map[string]any
	if err = json.Unmarshal(b, &s); err != nil {
		return false, false, err
	}
	found, sync := false, true
	for _, v := range entries(s) {
		e, _ := v.(map[string]any)
		hs, _ := e["hooks"].([]any)
		for _, h := range hs {
			m, _ := h.(map[string]any)
			if owned(m) {
				found = true
				if m["async"] == true || e["async"] == true || e["matcher"] != SpawnMatcher {
					sync = false
				}
			}
		}
	}
	return found, found && sync, nil
}
func Install(root, executable string, remove bool) error {
	installedCommand, err := ExecutableCommand(executable)
	if err != nil {
		return err
	}
	p := filepath.Join(root, "settings.json")
	b, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s := map[string]any{}
	if len(b) > 0 {
		if err = json.Unmarshal(b, &s); err != nil {
			return fmt.Errorf("settings: %w", err)
		}
		if s == nil {
			return fmt.Errorf("settings must be an object")
		}
	}
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		if s["hooks"] != nil {
			return fmt.Errorf("hooks must be an object")
		}
		hooks = map[string]any{}
	}
	if v, exists := hooks["PreToolUse"]; exists {
		if _, ok := v.([]any); !ok {
			return fmt.Errorf("PreToolUse hooks must be an array")
		}
	}
	var kept []any
	for _, v := range entries(s) {
		e, ok := v.(map[string]any)
		if !ok {
			kept = append(kept, v)
			continue
		}
		hs, ok := e["hooks"].([]any)
		if !ok {
			kept = append(kept, v)
			continue
		}
		var left []any
		for _, h := range hs {
			m, _ := h.(map[string]any)
			if !owned(m) && m["command"] != installedCommand {
				left = append(left, h)
			}
		}
		if len(left) > 0 {
			e["hooks"] = left
			kept = append(kept, e)
		}
	}
	if !remove {
		cmd, err := ExecutableCommand(executable)
		if err != nil {
			return err
		}
		kept = append(kept, map[string]any{"matcher": SpawnMatcher, "hooks": []any{map[string]any{"type": "command", "command": cmd}}})
	}
	if len(kept) > 0 {
		hooks["PreToolUse"] = kept
	} else {
		delete(hooks, "PreToolUse")
	}
	if len(hooks) > 0 {
		s["hooks"] = hooks
	} else {
		delete(s, "hooks")
	}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	var old any
	var next any
	_ = json.Unmarshal(b, &old)
	_ = json.Unmarshal(out, &next)
	ob, _ := json.Marshal(old)
	nb, _ := json.Marshal(next)
	if bytes.Equal(ob, nb) || (remove && len(b) == 0) {
		return nil
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if len(b) > 0 {
		backup := p + ".fanoutfuse.bak"
		if _, e := os.Stat(backup); os.IsNotExist(e) {
			if err = os.WriteFile(backup, b, 0600); err != nil {
				return err
			}
		}
	}
	f, err := os.CreateTemp(root, ".settings-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(out); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}
