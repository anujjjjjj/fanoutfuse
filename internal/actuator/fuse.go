// Package actuator contains local containment and process control operations.
package actuator

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

func ValidID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\\x00")
}
func Reason(v model.Verdict) string {
	unit := ""
	if v.Rule == "spawn_rate" {
		unit = "/min"
	}
	if v.Rule == "spawn_burst" {
		unit = "/10s"
	}
	return fmt.Sprintf("FanoutFuse tripped (%s %g%s > %g). Do not spawn subagents; complete remaining work directly.", v.Rule, v.Value, unit, v.Threshold)
}
func WriteFuse(home, sid, reason string) error {
	if !ValidID(sid) {
		return fmt.Errorf("invalid session id")
	}
	dir := filepath.Join(home, "fuse")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(home, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".fuse-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.WriteString(reason); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, sid))
}
func ClearFuse(home, sid string) error {
	if !ValidID(sid) {
		return fmt.Errorf("invalid session id")
	}
	err := os.Remove(filepath.Join(home, "fuse", sid))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func Guard(home string, in io.Reader, out io.Writer) error {
	var hook struct {
		Session string `json:"session_id"`
		Tool    string `json:"tool_name"`
	}
	if err := json.NewDecoder(io.LimitReader(in, 1<<20)).Decode(&hook); err != nil {
		return nil
	}
	if hook.Tool != "" && hook.Tool != "Agent" && hook.Tool != "Task" && hook.Tool != "Workflow" {
		return nil
	}
	if !ValidID(hook.Session) {
		return nil
	}
	for _, id := range []string{hook.Session, "ALL"} {
		p := filepath.Join(home, "fuse", id)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		b, err := os.ReadFile(p)
		reason := strings.TrimSpace(string(b))
		if err != nil || reason == "" {
			reason = "FanoutFuse tripped (containment active). Do not spawn subagents; complete remaining work directly."
		}
		response := struct {
			Output struct {
				Event    string `json:"hookEventName"`
				Decision string `json:"permissionDecision"`
				Reason   string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}{}
		response.Output.Event = "PreToolUse"
		response.Output.Decision = "deny"
		response.Output.Reason = reason
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(response)
	}
	return nil
}
