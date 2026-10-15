package replay

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/actuator"
	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/store"
	"github.com/anujjjjjj/fanoutfuse/internal/tui"
)

func TestFixtures(t *testing.T) {
	for _, name := range []string{"69332", "68619-burst", "77361-breadth", "77414-post-stop", "normal-pr-review", "planned-20"} {
		t.Run(name, func(t *testing.T) {
			c := config.Default()
			c.Home = t.TempDir()
			s, err := store.Open(c.Home)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			path := filepath.Join("..", "..", "fixtures", name+".yaml")
			f, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			// Every fixture carries tree and usage metadata, including safe sessions.
			depths := map[string]int{"main": 0}
			maxDepth := 0
			for _, e := range f.Events() {
				if e.Kind == "spawn" {
					depth := 1
					if e.Parent != "" {
						depth = depths[e.Parent] + 1
					}
					depths[e.Agent] = depth
					if depth > maxDepth {
						maxDepth = depth
					}
					if e.Type == "" {
						t.Fatal("missing subagent type")
					}
				}
			}
			wantDepth := 3
			if name == "77361-breadth" {
				wantDepth = 5
			}
			if maxDepth != wantDepth {
				t.Fatalf("depth %d, want %d", maxDepth, wantDepth)
			}
			var out bytes.Buffer
			start := time.Now()
			r, err := Run(context.Background(), path, c, s, &out)
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatal("replay must use virtual time")
			}
			if f.Expected == "none" {
				if r.Trip != nil || !strings.Contains(out.String(), "NO TRIP") {
					t.Fatal(out.String())
				}
				if !strings.Contains(out.String(), fmt.Sprintf("count-cap@50: never reached (%d agents)", r.Count)) || strings.Contains(out.String(), "would trip") {
					t.Fatal(out.String())
				}
				return
			}
			if r.Trip == nil || r.Trip.Rule != f.Expected {
				t.Fatalf("want %s: %s", f.Expected, out.String())
			}
			if r.Index >= 40 {
				t.Fatalf("late trip %d", r.Index)
			}
			if !strings.Contains(out.String(), fmt.Sprintf("tripped at agent %d; count-cap@50 would trip at agent 50 (%d earlier)", r.Index, 50-r.Index)) {
				t.Fatal(out.String())
			}
			stored, err := s.Get(r.Incident.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Total != r.Count || stored.After != r.Count-r.Index {
				t.Fatal("stored spawn accounting incorrect", stored.Total, stored.After)
			}
			if stored.TokensIn == 0 || stored.TokensOut == 0 || stored.TokensCache == 0 || stored.Cost == 0 {
				t.Fatal("missing usage")
			}
			actionSpawns := 0
			for i, a := range stored.Actions {
				if a.Step == "verify" {
					if i > 0 && stored.Actions[i-1].Step == "verify" {
						t.Fatal("duplicate verification")
					}
					continue
				}
				actionSpawns += a.SpawnsAfterTrip
			}
			if actionSpawns != stored.After {
				t.Fatalf("step counts %d != %d", actionSpawns, stored.After)
			}
			sum := 0
			for _, n := range stored.Buckets {
				sum += n
			}
			if sum != r.Count {
				t.Fatal("bucket count", sum)
			}
			var guard bytes.Buffer
			if err = actuator.Guard(c.Home, strings.NewReader(`{"session_id":"`+f.Session+`","tool_name":"Agent"}`), &guard); err != nil || !strings.Contains(guard.String(), `"permissionDecision":"deny"`) {
				t.Fatal("guard did not deny", guard.String(), err)
			}
			if name == "77414-post-stop" {
				pause := false
				for _, a := range stored.Actions {
					if a.Step == "pause" {
						pause = true
					}
					if a.Step == "stop_background" && a.SpawnsAfterTrip == 0 {
						t.Fatal("post-stop spawns not recorded")
					}
				}
				if !pause {
					t.Fatal("no escalation", out.String())
				}
			}
			out.Reset()
			if err = tui.Render(&out, stored, true); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"TRIPPED", "Spawns", "Tree (by depth)", "Top-5 branches", "Actions:", "Projected if unstopped"} {
				if !strings.Contains(out.String(), field) {
					t.Fatal("missing pm field", field)
				}
			}
		})
	}
}
