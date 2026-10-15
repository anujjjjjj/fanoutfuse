// Package claude reads session and transcript metadata from Claude's local files.
package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

func Sessions(root string) ([]model.Session, []string) {
	var out []model.Session
	var warnings []string
	paths, _ := filepath.Glob(filepath.Join(root, "sessions", "*.json"))
	if len(paths) == 0 {
		warnings = append(warnings, "undocumented sessions/*.json registry missing; using process scan fallback")
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		var raw struct {
			model.Session
			CamelID string `json:"sessionId"`
		}
		if err = json.Unmarshal(b, &raw); err != nil {
			warnings = append(warnings, "undocumented registry schema unreadable: "+filepath.Base(p))
			continue
		}
		s := raw.Session
		if s.ID == "" {
			s.ID = raw.CamelID
		}
		if s.PID == 0 {
			s.PID, _ = strconv.Atoi(strings.TrimSuffix(filepath.Base(p), ".json"))
		}
		if s.ID == "" || s.PID <= 1 {
			warnings = append(warnings, "undocumented registry fields missing: "+filepath.Base(p))
			continue
		}
		if syscall.Kill(s.PID, 0) == nil {
			out = append(out, s)
		}
	}
	return out, warnings
}
func ScanProcesses() []model.Session {
	b, err := exec.Command("ps", "-axo", "pid=,tty=,args=").Output()
	if err != nil {
		return nil
	}
	var out []model.Session
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		base := filepath.Base(f[2])
		if base != "claude" {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		if pid <= 1 {
			continue
		}
		s := model.Session{ID: f[0], PID: pid, TTY: f[1]}
		if cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
			s.CWD = cwd
		} else if cwd, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output(); err == nil {
			for _, line := range strings.Split(string(cwd), "\n") {
				if strings.HasPrefix(line, "n/") {
					s.CWD = line[1:]
					break
				}
			}
		}
		for i := 3; i+1 < len(f); i++ {
			if f[i] == "--resume" || f[i] == "--session-id" {
				s.ID = f[i+1]
			}
		}
		out = append(out, s)
	}
	return out
}
func Resolve(root, id string) (model.Session, error) {
	ss, _ := Sessions(root)
	for _, s := range ss {
		if s.ID == id || strconv.Itoa(s.PID) == id {
			return s, nil
		}
	}
	if pid, err := strconv.Atoi(id); err == nil && pid > 1 {
		if syscall.Kill(pid, 0) == nil {
			return model.Session{ID: id, PID: pid}, nil
		}
	}
	processes := ScanProcesses()
	for _, s := range processes {
		if s.ID == id {
			return s, nil
		}
	}
	// A registry-free foreground command usually has no session id in argv.
	// Match its working directory to metadata from the session's main transcript.
	paths, _ := filepath.Glob(filepath.Join(root, "projects", "*", id+".jsonl"))
	var candidates []model.Session
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		cwd := ""
		for n := 0; n < 50 && scanner.Scan(); n++ {
			var row struct {
				CWD string `json:"cwd"`
			}
			if json.Unmarshal(scanner.Bytes(), &row) == nil && row.CWD != "" {
				cwd = row.CWD
				break
			}
		}
		file.Close()
		if cwd == "" {
			continue
		}
		for _, s := range processes {
			if s.CWD == cwd {
				s.ID = id
				candidates = append(candidates, s)
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		return model.Session{}, fmt.Errorf("session %q has ambiguous process matches; use a raw pid", id)
	}
	return model.Session{}, fmt.Errorf("no live session %q found", id)
}
func Live(root string) []model.Session {
	ss, _ := Sessions(root)
	seen := map[int]bool{}
	for _, s := range ss {
		seen[s.PID] = true
	}
	for _, s := range ScanProcesses() {
		if !seen[s.PID] {
			ss = append(ss, s)
		}
	}
	return ss
}
