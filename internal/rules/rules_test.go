package rules

import (
	"fmt"
	"testing"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/model"
	"github.com/anujjjjjj/fanoutfuse/internal/tree"
)

func TestEveryRule(t *testing.T) {
	for _, rule := range config.Names {
		t.Run(rule, func(t *testing.T) {
			c := config.Default()
			for n, l := range c.Limits {
				l.Trip = 0
				l.Warn = 0
				c.Limits[n] = l
			}
			c.Limits[rule] = config.Limit{Warn: 1, Trip: 2, MinTrip: 2}
			tr := tree.New()
			at := time.Unix(1000, 0)
			switch rule {
			case "growth_factor":
				tr.Apply(model.Event{Kind: "spawn", Agent: "prev", At: at.Add(-40 * time.Second)})
			case "tokens_per_min":
				tr.Apply(model.Event{Kind: "usage", Agent: "x", At: at, TokensIn: 2})
			}
			for i := 0; i < 2; i++ {
				parent := "main"
				if rule == "max_depth" && i > 0 {
					parent = "a0"
				}
				tr.Apply(model.Event{Kind: "spawn", Agent: fmt.Sprintf("a%d", i), Parent: parent, At: at})
			}
			e := Engine{Config: c}
			v := e.Evaluate(tr, at)
			if v == nil || v.Level != "trip" || v.Rule != rule {
				t.Fatalf("got %+v", v)
			}
			if e.Evaluate(tr, at) != nil {
				t.Fatal("trip did not latch")
			}
		})
	}
}
func TestWindowBoundariesAndHysteresis(t *testing.T) {
	c := config.Default()
	for n := range c.Limits {
		c.Limits[n] = config.Limit{}
	}
	c.Limits["spawn_burst"] = config.Limit{Warn: 1, Trip: 3}
	at := time.Unix(1000, 0)
	tr := tree.New()
	tr.Apply(model.Event{Kind: "spawn", Agent: "a", At: at})
	e := Engine{Config: c}
	if v := e.Evaluate(tr, at); v == nil || v.Level != "warn" {
		t.Fatal(v)
	}
	if e.Evaluate(tr, at.Add(time.Second)) != nil {
		t.Fatal("warn repeated")
	}
	if tr.Metrics(at.Add(10 * time.Second))["spawn_burst"] != 0 {
		t.Fatal("expired burst counted")
	}
	if tr.Metrics(at.Add(60 * time.Second))["spawn_rate"] != 0 {
		t.Fatal("expired rate counted")
	}
	e.Tripped = true
	e.Resume(at)
	if e.Tripped {
		t.Fatal("resume did not reset latch")
	}
	tr.Apply(model.Event{Kind: "spawn", Agent: "b", At: at})
	tr.Apply(model.Event{Kind: "spawn", Agent: "c", At: at})
	if v := e.Evaluate(tr, at); v != nil && v.Level == "trip" {
		t.Fatal("cooldown not applied")
	}
}
func TestGrowthRequiresNonzeroPreviousAndMinimum(t *testing.T) {
	c := config.Default()
	for n := range c.Limits {
		if n != "growth_factor" {
			c.Limits[n] = config.Limit{}
		}
	}
	tr := tree.New()
	at := time.Unix(1000, 0)
	for i := 0; i < 30; i++ {
		tr.Apply(model.Event{Kind: "spawn", Agent: fmt.Sprint(i), At: at})
	}
	e := Engine{Config: c}
	if v := e.Evaluate(tr, at); v != nil {
		t.Fatal("zero denominator must not imply infinite growth", v)
	}
}
