// Package rules implements deterministic sliding-window rules and latched trips.
package rules

import (
	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/model"
	"github.com/anujjjjjj/fanoutfuse/internal/tree"
	"time"
)

type Engine struct {
	Config        config.Config
	Tripped       bool
	LastWarn      time.Time
	CooldownUntil time.Time
}

func (e *Engine) Evaluate(t *tree.Tree, at time.Time) *model.Verdict {
	if e.Tripped {
		return nil
	}
	m := t.Metrics(at)
	factor := 1.0
	if at.Before(e.CooldownUntil) {
		factor = 2
	}
	for _, level := range []string{"trip", "warn"} {
		for _, n := range config.Names {
			l := e.Config.Limits[n]
			threshold := l.Trip * factor
			minN := l.MinTrip
			if level == "warn" {
				threshold = l.Warn * factor
				minN = l.MinWarn
			}
			if threshold <= 0 || m[n] < threshold {
				continue
			}
			if n == "growth_factor" && m["growth_n"] < float64(minN) {
				continue
			}
			if level == "warn" {
				if !e.LastWarn.IsZero() && at.Sub(e.LastWarn) < time.Minute {
					continue
				}
				e.LastWarn = at
			} else {
				e.Tripped = true
			}
			return &model.Verdict{Level: level, Rule: n, Value: m[n], Threshold: threshold, At: at}
		}
	}
	return nil
}
func (e *Engine) Resume(at time.Time) {
	e.Tripped = false
	d, _ := time.ParseDuration(e.Config.Cooldown.AfterResume)
	e.CooldownUntil = at.Add(d)
}
