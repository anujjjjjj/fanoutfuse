package actuator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

func TestGuardExactAndIndependent(t *testing.T) {
	home := t.TempDir()
	reason := Reason(model.Verdict{Rule: "spawn_rate", Value: 34, Threshold: 30})
	if err := WriteFuse(home, "s1", reason); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Guard(home, strings.NewReader(`{"session_id":"s1","tool_name":"Agent"}`), &out); err != nil {
		t.Fatal(err)
	}
	want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"FanoutFuse tripped (spawn_rate 34/min > 30). Do not spawn subagents; complete remaining work directly."}}` + "\n"
	if out.String() != want {
		t.Fatalf("%s", out.String())
	}
	for _, input := range []string{`{"session_id":"other"}`, `{"session_id":"s1","tool_name":"Read"}`, `{"session_id":"../../s1"}`, `{`} {
		out.Reset()
		if err := Guard(home, strings.NewReader(input), &out); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Fatalf("unexpected deny: %s", input)
		}
	}
	if err := WriteFuse(home, "ALL", reason); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_ = Guard(home, strings.NewReader(`{"session_id":"other"}`), &out)
	if out.Len() == 0 {
		t.Fatal("ALL ignored")
	}
	info, err := os.Stat(filepath.Join(home, "fuse"))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("fuse directory permissions")
	}
}
func TestGuardLatency(t *testing.T) {
	home := t.TempDir()
	if err := WriteFuse(home, "bench", "reason"); err != nil {
		t.Fatal(err)
	}
	samples := make([]time.Duration, 500)
	for i := range samples {
		start := time.Now()
		if err := Guard(home, strings.NewReader(`{"session_id":"bench"}`), io.Discard); err != nil {
			t.Fatal(err)
		}
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p99 := samples[len(samples)*99/100]
	t.Logf("guard p99=%s", p99)
	if p99 >= 5*time.Millisecond {
		t.Fatalf("guard p99 >=5ms: %s", p99)
	}
}
func BenchmarkGuard(b *testing.B) {
	home := b.TempDir()
	if err := WriteFuse(home, "bench", "reason"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Guard(home, strings.NewReader(`{"session_id":"bench"}`), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
func TestInstallIdempotentPreservesSettings(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "settings.json")
	original := []byte(`{"theme":"dark","hooks":{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"echo other"}]}],"Stop":[{"matcher":"*"}]}}`)
	if err := os.WriteFile(p, original, 0600); err != nil {
		t.Fatal(err)
	}
	bin := "/tmp/with spaces/fanoutfuse"
	if err := Install(root, bin, false); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(p)
	if err := Install(root, bin, false); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(p)
	if !bytes.Equal(first, second) {
		t.Fatal("not idempotent")
	}
	backup, _ := os.ReadFile(p + ".fanoutfuse.bak")
	if !bytes.Equal(backup, original) {
		t.Fatal("backup changed")
	}
	installed, sync, err := HookHealth(root)
	if err != nil || !installed || !sync {
		t.Fatal(installed, sync, err)
	}
	var doc map[string]any
	_ = json.Unmarshal(first, &doc)
	if doc["theme"] != "dark" {
		t.Fatal("lost setting")
	}
	if err = Install(root, bin, true); err != nil {
		t.Fatal(err)
	}
	if err = Install(root, bin, true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	var a, b any
	_ = json.Unmarshal(original, &a)
	_ = json.Unmarshal(got, &b)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("uninstall changed other settings: %s", got)
	}
}
func TestDoctorRejectsAsync(t *testing.T) {
	root := t.TempDir()
	for _, extra := range []string{`,"async":true`, ``} {
		s := `{"hooks":{"PreToolUse":[{"matcher":"^(Task|Agent)$","hooks":[{"type":"command","command":"/tmp/fanoutfuse guard"` + extra + `}]}]}}`
		_ = os.WriteFile(filepath.Join(root, "settings.json"), []byte(s), 0600)
		found, sync, err := HookHealth(root)
		if err != nil || !found || sync != (extra == "") {
			t.Fatal(found, sync, err)
		}
	}
}
