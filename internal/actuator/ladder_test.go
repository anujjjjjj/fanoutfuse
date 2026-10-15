package actuator

import (
	"context"
	"testing"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/config"
	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

func TestVerificationCountsPerStep(t *testing.T) {
	c := config.Default()
	c.Home = t.TempDir()
	c.General.Ladder = []string{"contain", "stop_background", "pause"}
	in := &model.Incident{}
	l := &Ladder{Config: c, Session: model.Session{ID: "test"}, Incident: in, Simulated: true}
	ctx := context.Background()
	at := time.Now()
	l.Start(ctx, at)
	spawn := func(n int) {
		for i := 0; i < n; i++ {
			in.After++
			l.ObserveSpawn()
		}
	}
	spawn(3)
	l.Advance(ctx, at.Add(20*time.Second))
	spawn(2)
	l.Advance(ctx, at.Add(40*time.Second))
	spawn(4)
	l.Advance(ctx, at.Add(60*time.Second))
	l.Advance(ctx, at.Add(80*time.Second))
	wantSteps := []string{"contain", "stop_background", "verify", "pause", "verify"}
	wantCounts := []int{0, 3, 3, 6, 6}
	if len(in.Actions) != len(wantSteps) {
		t.Fatal(in.Actions)
	}
	for i, a := range in.Actions {
		if a.Step != wantSteps[i] || a.SpawnsAfterTrip != wantCounts[i] {
			t.Fatal(in.Actions)
		}
	}
	if l.Waiting || in.Contained.IsZero() {
		t.Fatal("containment not verified")
	}
}
