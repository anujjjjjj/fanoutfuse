// Package daemon connects metadata adapters, rules, containment, and storage.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/actuator"
	"github.com/anujjjjjj/fanoutfuse/internal/adapter/claude"
	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/ctl"
	"github.com/anujjjjjj/fanoutfuse/internal/model"
	"github.com/anujjjjjj/fanoutfuse/internal/rules"
	"github.com/anujjjjjj/fanoutfuse/internal/store"
	"github.com/anujjjjjj/fanoutfuse/internal/tree"
)

type State struct {
	Tree      *tree.Tree
	Engine    rules.Engine
	Incident  *model.Incident
	Ladder    *actuator.Ladder
	Session   model.Session
	Warned    time.Time
	LastFlush time.Time
}
type Detector struct {
	Config     config.Config
	Sessions   map[string]*State
	Simulated  bool
	Store      *store.Store
	CLIVersion string
}

func New(c config.Config, s *store.Store, sim bool) *Detector {
	version := "replay/v1"
	if !sim {
		version = claude.Version(context.Background())
	}
	return &Detector{Config: c, Store: s, Simulated: sim, Sessions: map[string]*State{}, CLIVersion: version}
}
func (d *Detector) State(sid string) *State {
	s := d.Sessions[sid]
	if s == nil {
		c := d.Config
		session := model.Session{ID: sid}
		if !d.Simulated {
			if live, err := claude.Resolve(c.ClaudeRoot, sid); err == nil {
				session = live
				if session.CWD != "" {
					if resolved, err := config.Load(session.CWD); err == nil {
						c = resolved
					}
				}
			}
		}
		s = &State{Tree: tree.New(), Engine: rules.Engine{Config: c}, Session: session}
		d.Sessions[sid] = s
	}
	return s
}
func NewID(at time.Time) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%013d-%s", at.UnixMilli(), hex.EncodeToString(b[:]))
}
func (d *Detector) Apply(ctx context.Context, e model.Event) (*model.Verdict, error) {
	s := d.State(e.Session)
	// Expire the previous verification window before attributing this event to the next step.
	if s.Ladder != nil {
		s.Ladder.Advance(ctx, e.At)
	}
	spawned := s.Tree.Apply(e)
	if s.Incident != nil && spawned && !e.At.Before(s.Incident.Tripped) {
		s.Incident.After++
		if s.Ladder != nil {
			s.Ladder.ObserveSpawn()
		}
		if s.Ladder != nil && !s.Ladder.Waiting {
			s.Ladder.Waiting = true
			s.Ladder.Baseline = s.Incident.After - 1
			s.Ladder.WindowStart = len(s.Incident.Actions)
			s.Ladder.Deadline = e.At.Add(s.Engine.Config.EscalateAfter())
		}
		s.Incident.Contained = time.Time{}
	}
	v := s.Engine.Evaluate(s.Tree, e.At)
	if v != nil {
		if v.Level == "warn" {
			s.Warned = e.At
			if !d.Simulated && s.Engine.Config.General.Notify {
				actuator.Notify(ctx, fmt.Sprintf("session %s: %s %g", e.Session, v.Rule, v.Value), s.Engine.Config.General.Bell)
			}
		} else {
			d.trip(ctx, s, *v)
		}
	}
	if s.Incident != nil {
		d.snapshot(s)
		if d.Store != nil {
			return v, d.Store.Save(s.Incident)
		}
	}
	return v, nil
}
func (d *Detector) trip(ctx context.Context, s *State, v model.Verdict) {
	in := &model.Incident{ID: NewID(v.At), Platform: "claude", Version: d.CLIVersion, Session: s.Session.ID, Project: s.Session.CWD, Started: s.Tree.First, Warned: s.Warned, Tripped: v.At, Rule: v.Rule, Value: v.Value, Threshold: v.Threshold, Buckets: map[int64]int{}}
	if in.Started.IsZero() {
		in.Started = v.At
	}
	s.Incident = in
	s.Engine.Tripped = true
	m := s.Tree.Metrics(v.At)
	in.Projected = len(s.Tree.Spawns) + int(m["spawn_rate"]*5)
	s.Ladder = &actuator.Ladder{Config: s.Engine.Config, Session: s.Session, Incident: in, Simulated: d.Simulated}
	s.Ladder.Start(ctx, v.At)
	if !d.Simulated && s.Engine.Config.General.Notify {
		actuator.Notify(ctx, actuator.Reason(v), s.Engine.Config.General.Bell)
	}
}
func (d *Detector) snapshot(s *State) {
	in := s.Incident
	in.Nodes = s.Tree.Snapshot()
	in.Total = len(s.Tree.Spawns)
	in.MaxDepth = int(s.Tree.Metrics(in.Tripped)["max_depth"])
	in.TokensIn = 0
	in.TokensOut = 0
	in.TokensCache = 0
	for _, n := range in.Nodes {
		in.TokensIn += n.TokensIn
		in.TokensOut += n.TokensOut
		in.TokensCache += n.TokensCache
	}
	in.Cost = float64(in.TokensIn)*3/1e6 + float64(in.TokensOut)*15/1e6 + float64(in.TokensCache)*0.3/1e6
	in.Buckets = map[int64]int{}
	for _, at := range s.Tree.Spawns {
		in.Buckets[at.UnixMilli()/10000*10000]++
	}
}
func (d *Detector) Tick(ctx context.Context, at time.Time) error {
	for _, s := range d.Sessions {
		if v := s.Engine.Evaluate(s.Tree, at); v != nil {
			if v.Level == "trip" {
				d.trip(ctx, s, *v)
			} else {
				s.Warned = at
				if !d.Simulated && s.Engine.Config.General.Notify {
					actuator.Notify(ctx, fmt.Sprintf("session %s: %s %g", s.Session.ID, v.Rule, v.Value), s.Engine.Config.General.Bell)
				}
			}
		}
		if d.Store != nil && (s.LastFlush.IsZero() || at.Sub(s.LastFlush) >= 30*time.Second) {
			if err := d.Store.SaveSnapshot(s.Session.ID, s.Tree.Snapshot()); err != nil {
				return err
			}
			s.LastFlush = at
		}
		if s.Ladder != nil {
			due := s.Ladder.Waiting && !at.Before(s.Ladder.Deadline)
			s.Ladder.Advance(ctx, at)
			if due && d.Store != nil {
				d.snapshot(s)
				if err := d.Store.Save(s.Incident); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (d *Detector) Control(ctx context.Context, method string, args []string) (string, error) {
	switch method {
	case "status":
		var b strings.Builder
		fmt.Fprintf(&b, "FanoutFuse running · %d monitored sessions\n", len(d.Sessions))
		for id, s := range d.Sessions {
			fmt.Fprintf(&b, "%s spawns=%d tripped=%t\n", id, len(s.Tree.Spawns), s.Engine.Tripped)
		}
		return b.String(), nil
	case "stop":
		ss := claude.Live(d.Config.ClaudeRoot)
		if err := actuator.WriteFuse(d.Config.Home, "ALL", "FanoutFuse tripped (manual stop). Do not spawn subagents; complete remaining work directly."); err != nil {
			return "", err
		}
		for _, live := range ss {
			s := d.State(live.ID)
			s.Session = live
			if s.Incident == nil {
				d.trip(ctx, s, model.Verdict{Rule: "manual_stop", At: time.Now()})
				d.snapshot(s)
				if err := d.Store.Save(s.Incident); err != nil {
					return "", err
				}
			} else {
				// The panic button retries containment even for an already tripped tree.
				c := s.Engine.Config
				c.General.Ladder = []string{"contain", "stop_background"}
				s.Ladder = &actuator.Ladder{Config: c, Session: live, Incident: s.Incident}
				s.Ladder.Start(ctx, time.Now())
				if err := d.Store.Save(s.Incident); err != nil {
					return "", err
				}
			}
		}
		return fmt.Sprintf("Contained %d live sessions (global fuse active)", len(ss)), nil
	case "pause", "resume":
		if len(args) != 1 {
			return "", fmt.Errorf("%s requires a session or pid", method)
		}
		live, err := claude.Resolve(d.Config.ClaudeRoot, args[0])
		if err != nil {
			return "", err
		}
		sig := syscall.SIGSTOP
		if method == "resume" {
			sig = syscall.SIGCONT
		}
		if err = actuator.SignalGroup(live.PID, sig); err != nil {
			return "", err
		}
		s := d.State(live.ID)
		if method == "resume" {
			if err = actuator.ClearFuse(d.Config.Home, live.ID); err != nil {
				return "", err
			}
			if err = actuator.ClearFuse(d.Config.Home, "ALL"); err != nil {
				return "", err
			}
			s.Engine.Resume(time.Now())
			if s.Incident != nil {
				s.Incident.Resolution = "resumed"
			}
		}
		if s.Incident != nil {
			s.Incident.Actions = append(s.Incident.Actions, model.Action{Step: method, At: time.Now(), OK: true, SpawnsAfterTrip: s.Incident.After})
			if err = d.Store.Save(s.Incident); err != nil {
				return "", err
			}
		}
		if method == "resume" {
			s.Incident = nil
			s.Ladder = nil
		}
		return method + " " + live.ID, nil
	default:
		return "", fmt.Errorf("unknown method %q", method)
	}
}
func Run(ctx context.Context, c config.Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l, err := ctl.Listen(c.Home)
	if err != nil {
		return err
	}
	defer l.Close()
	defer os.Remove(c.Home + "/ctl.sock")
	s, err := store.Open(c.Home)
	if err != nil {
		return err
	}
	defer s.Close()
	d := New(c, s, false)
	events := make(chan model.Event, 1024)
	calls := make(chan ctl.Call)
	errs := make(chan error, 2)
	go func() { errs <- ctl.Serve(ctx, l, calls) }()
	w := &claude.Watcher{Root: c.ClaudeRoot}
	go func() { errs <- w.Watch(ctx, events) }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errs:
			return err
		case e := <-events:
			if _, err = d.Apply(ctx, e); err != nil {
				return err
			}
		case at := <-tick.C:
			if err = d.Tick(ctx, at); err != nil {
				return err
			}
		case call := <-calls:
			result, err := d.Control(ctx, call.Request.Method, call.Request.Params)
			res := ctl.Response{JSONRPC: "2.0", ID: call.Request.ID, Result: result}
			if err != nil {
				res.Error = &ctl.RPCError{Code: -32000, Message: err.Error()}
			}
			call.Reply <- res
		}
	}
}
