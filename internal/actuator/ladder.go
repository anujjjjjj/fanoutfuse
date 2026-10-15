package actuator

import (
	"context"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

// Ladder is advanced by live or virtual timestamps. Live pause checks wait for process state.
type Ladder struct {
	Config      config.Config
	Session     model.Session
	Incident    *model.Incident
	Simulated   bool
	Waiting     bool
	Deadline    time.Time
	Baseline    int
	Next        int
	WindowStart int
}

func (l *Ladder) record(step string, at time.Time, err error) {
	a := model.Action{Step: step, At: at, OK: err == nil, SpawnsAfterTrip: 0}
	if err != nil {
		a.Error = err.Error()
	}
	l.Incident.Actions = append(l.Incident.Actions, a)
}
func (l *Ladder) execute(ctx context.Context, step string, at time.Time) error {
	var err error
	switch step {
	case "contain":
		err = WriteFuse(l.Config.Home, l.Session.ID, Reason(model.Verdict{Rule: l.Incident.Rule, Value: l.Incident.Value, Threshold: l.Incident.Threshold}))
	case "stop_background":
		if !l.Simulated {
			err = stopBackground(ctx, l.Session, func(step string, err error) { l.record(step, at, err) })
		}
	case "pause":
		if !l.Simulated {
			err = PauseGroup(ctx, l.Session.PID)
		}
	case "kill":
		if !l.Simulated {
			err = KillGroup(ctx, l.Session.PID, func(s string, e error) { l.record(s, time.Now(), e) })
		}
	}
	l.record(step, at, err)
	return err
}
func (l *Ladder) Start(ctx context.Context, at time.Time) {
	// Containment and the graceful stop are immediate. The process-group steps require verification first.
	for l.Next < len(l.Config.General.Ladder) {
		s := l.Config.General.Ladder[l.Next]
		if s == "pause" || s == "kill" {
			break
		}
		l.Next++
		if s == "warn" {
			continue
		}
		l.execute(ctx, s, at)
	}
	l.WindowStart = 0
	l.Waiting = true
	l.Baseline = l.Incident.After
	l.Deadline = at.Add(l.Config.EscalateAfter())
}
func (l *Ladder) Advance(ctx context.Context, at time.Time) {
	if !l.Waiting || at.Before(l.Deadline) {
		return
	}
	continued := l.Incident.After > l.Baseline
	// Verification summarizes the latest step, including later observation windows.
	actions := l.Incident.Actions
	count := 0
	for i := len(actions) - 1; i >= 0; i-- {
		if actions[i].Step != "verify" {
			count = actions[i].SpawnsAfterTrip
			break
		}
	}
	if len(actions) > 0 && actions[len(actions)-1].Step == "verify" {
		a := &l.Incident.Actions[len(actions)-1]
		a.At = l.Deadline
		a.SpawnsAfterTrip = count
	} else {
		l.record("verify", l.Deadline, nil)
		l.Incident.Actions[len(l.Incident.Actions)-1].SpawnsAfterTrip = count
	}
	if !continued {
		l.Incident.Contained = l.Deadline
		l.Waiting = false
		return
	}
	if l.Next >= len(l.Config.General.Ladder) {
		l.WindowStart = len(l.Incident.Actions)
		l.Baseline = l.Incident.After
		l.Deadline = at.Add(l.Config.EscalateAfter())
		return
	}
	l.WindowStart = len(l.Incident.Actions)
	for l.Next < len(l.Config.General.Ladder) {
		s := l.Config.General.Ladder[l.Next]
		l.Next++
		if err := l.execute(ctx, s, l.Deadline); err == nil || s != "pause" {
			break
		}
	}
	l.Baseline = l.Incident.After
	l.Deadline = l.Deadline.Add(l.Config.EscalateAfter())
}

// ObserveSpawn attributes a spawn to the most recent ladder action.
func (l *Ladder) ObserveSpawn() {
	for i := len(l.Incident.Actions) - 1; i >= 0; i-- {
		if l.Incident.Actions[i].Step != "verify" {
			l.Incident.Actions[i].SpawnsAfterTrip++
			return
		}
	}
}
