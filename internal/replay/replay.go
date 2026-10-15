// Package replay exercises production detection and containment with virtual time.
package replay

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/actuator"
	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/daemon"
	"github.com/anujjjjjj/fanoutfuse/internal/model"
	"github.com/anujjjjjj/fanoutfuse/internal/store"
	"gopkg.in/yaml.v3"
)

type Agent struct {
	At       float64 `yaml:"at"`
	ID       string  `yaml:"id"`
	Parent   string  `yaml:"parent"`
	Duration float64 `yaml:"duration"`
	Tokens   int64   `yaml:"tokens"`
}
type Fixture struct {
	Session   string    `yaml:"session"`
	Start     time.Time `yaml:"start"`
	PerMinute []int     `yaml:"per_minute"`
	Window    float64   `yaml:"dispatch_window_seconds"`
	Agents    []Agent   `yaml:"agents"`
	Ladder    []string  `yaml:"action_ladder"`
	Expected  string    `yaml:"expected"`
}
type Result struct {
	Trip     *model.Verdict
	Index    int
	Incident *model.Incident
	Count    int
}

func Load(path string) (Fixture, error) {
	var f Fixture
	b, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	err = yaml.Unmarshal(b, &f)
	if f.Session == "" {
		f.Session = filepath.Base(path)
	}
	if f.Start.IsZero() {
		f.Start = time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	}
	return f, err
}
func (f Fixture) Events() []model.Event {
	agents := append([]Agent(nil), f.Agents...)
	index := 0
	window := f.Window
	if window <= 0 {
		window = 59
	}
	for minute, n := range f.PerMinute {
		for j := 0; j < n; j++ {
			index++
			agents = append(agents, Agent{ID: fmt.Sprintf("a%04d", index), At: float64(minute*60) + float64(j)*window/float64(n), Duration: 1})
		}
	}
	var events []model.Event
	for i, a := range agents {
		if a.ID == "" {
			a.ID = fmt.Sprintf("a%04d", i+1)
		}
		at := f.Start.Add(time.Duration(a.At * float64(time.Second)))
		events = append(events, model.Event{Kind: "spawn", Session: f.Session, Agent: a.ID, Parent: a.Parent, At: at})
		if a.Duration > 0 {
			events = append(events, model.Event{Kind: "end", Session: f.Session, Agent: a.ID, At: at.Add(time.Duration(a.Duration * float64(time.Second)))})
		}
		if a.Tokens > 0 {
			events = append(events, model.Event{Kind: "usage", Session: f.Session, Agent: a.ID, At: at, TokensIn: a.Tokens})
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	return events
}
func Run(ctx context.Context, path string, c config.Config, s *store.Store, out io.Writer) (Result, error) {
	var r Result
	f, err := Load(path)
	if err != nil {
		return r, err
	}
	if !actuator.ValidID(f.Session) {
		return r, fmt.Errorf("invalid fixture session id")
	}
	for _, agent := range f.Agents {
		if agent.ID != "" && !actuator.ValidID(agent.ID) {
			return r, fmt.Errorf("invalid fixture agent id")
		}
		if agent.At < 0 || agent.Duration < 0 || agent.Tokens < 0 {
			return r, fmt.Errorf("fixture timestamps, durations, and tokens must be nonnegative")
		}
	}
	if len(f.Ladder) > 0 && os.Getenv("FANOUTFUSE_ACTION_LADDER") == "" {
		c.General.Ladder = f.Ladder
	}
	// Commit the final replay atomically. Live detection persists changes as they
	// happen; replay has no real-time observer and can batch the same metadata.
	d := daemon.New(c, nil, true)
	events := f.Events()
	dir, err := os.MkdirTemp(c.Home, "replay-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(dir)
	sub := filepath.Join(dir, f.Session, "subagents")
	if err = os.MkdirAll(sub, 0700); err != nil {
		return r, err
	}
	for _, e := range events {
		select {
		case <-ctx.Done():
			return r, ctx.Err()
		default:
		}
		if e.Kind == "spawn" {
			r.Count++
			if err = os.WriteFile(filepath.Join(sub, "agent-"+e.Agent+".jsonl"), []byte("{}\n"), 0600); err != nil {
				return r, err
			}
			if err = os.Chtimes(filepath.Join(sub, "agent-"+e.Agent+".jsonl"), e.At, e.At); err != nil {
				return r, err
			}
		}
		v, err := d.Apply(ctx, e)
		if err != nil {
			return r, err
		}
		if v != nil && v.Level == "trip" && r.Trip == nil {
			r.Trip = v
			r.Index = r.Count
			fmt.Fprintf(out, "TRIP rule=%s value=%g threshold=%g at agent %d (%s)\n", v.Rule, v.Value, v.Threshold, r.Index, v.At.Format(time.RFC3339Nano))
		}
	}
	if len(events) > 0 {
		at := events[len(events)-1].At
		for i := 0; i < len(c.General.Ladder)+1; i++ {
			at = at.Add(c.EscalateAfter())
			if err = d.Tick(ctx, at); err != nil {
				return r, err
			}
		}
	}
	if r.Trip == nil {
		fmt.Fprintln(out, "NO TRIP")
	}
	fmt.Fprintln(out, "count-cap@50 would trip at agent 50")
	if r.Count < 50 {
		fmt.Fprintf(out, "count cap not reached (%d agents)\n", r.Count)
	}
	if state := d.Sessions[f.Session]; state != nil {
		r.Incident = state.Incident
	}
	if r.Incident != nil {
		if s != nil {
			if err = s.Save(r.Incident); err != nil {
				return r, err
			}
		}
		for _, a := range r.Incident.Actions {
			fmt.Fprintf(out, "step=%s at=%s ok=%t spawns_after_trip=%d\n", a.Step, a.At.Format(time.RFC3339Nano), a.OK, a.SpawnsAfterTrip)
		}
		fmt.Fprintf(out, "incident %s · total=%d spawns_after_trip=%d\n", r.Incident.ID, r.Count, r.Incident.After)
	}
	return r, nil
}
