// Package tree maintains session attribution without retaining transcript text.
package tree

import (
	"sort"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

type Tree struct {
	Nodes  map[string]*model.Node
	Spawns []time.Time
	Usage  []model.Event
	Seen   map[string]bool
	First  time.Time
}

func New() *Tree {
	return &Tree{Nodes: map[string]*model.Node{"main": {ID: "main", Type: "main", Depth: 0, Status: "unknown"}}, Seen: map[string]bool{}}
}
func (t *Tree) Apply(e model.Event) bool {
	if e.Agent == "" {
		e.Agent = "main"
	}
	n := t.Nodes[e.Agent]
	if n == nil {
		n = &model.Node{ID: e.Agent, Depth: 1, Status: "unknown", Created: e.At}
		if e.Agent == "main" {
			n.Depth = 0
		}
		t.Nodes[e.Agent] = n
	}
	n.Last = e.At
	switch e.Kind {
	case "spawn":
		if t.Seen[e.Agent] {
			return false
		}
		t.Seen[e.Agent] = true
		n.Status = "running"
		n.Created = e.At
		t.Spawns = append(t.Spawns, e.At)
		if t.First.IsZero() || e.At.Before(t.First) {
			t.First = e.At
		}
	case "end":
		n.Status = "done"
	case "usage":
		n.TokensIn += e.TokensIn
		n.TokensOut += e.TokensOut
		n.TokensCache += e.TokensCache
		t.Usage = append(t.Usage, e)
	case "intent":
		n.SpawnCalls++
	}
	if e.Parent != "" {
		n.Parent = e.Parent
	}
	if e.SpawnDepth != nil {
		d := *e.SpawnDepth
		n.SpawnDepth = &d
	}
	if e.Agent == "main" {
		n.Type = "main"
	} else if e.Type != "" {
		n.Type = e.Type
	}
	if e.Model != "" {
		n.Model = e.Model
	}
	n.Background = n.Background || e.Background
	t.depths()
	return e.Kind == "spawn"
}
func (t *Tree) depths() {
	var depth func(string, map[string]bool) int
	depth = func(id string, seen map[string]bool) int {
		if id == "main" {
			return 0
		}
		n := t.Nodes[id]
		if n != nil && n.SpawnDepth != nil {
			return *n.SpawnDepth
		}
		if n == nil || n.Parent == "" || seen[id] {
			return 1
		}
		seen[id] = true
		return 1 + depth(n.Parent, seen)
	}
	for id, n := range t.Nodes {
		n.Depth = depth(id, map[string]bool{})
	}
}
func (t *Tree) Snapshot() []model.Node {
	out := make([]model.Node, 0, len(t.Nodes))
	for _, n := range t.Nodes {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (t *Tree) Metrics(now time.Time) map[string]float64 {
	m := map[string]float64{"total_spawns": float64(len(t.Spawns))}
	var recent, prev int
	for _, at := range t.Spawns {
		d := now.Sub(at)
		if d < 0 {
			continue
		}
		if d < 60*time.Second {
			m["spawn_rate"]++
		}
		if d < 10*time.Second {
			m["spawn_burst"]++
		}
		if d < 30*time.Second {
			recent++
		} else if d < 60*time.Second {
			prev++
		}
	}
	if prev > 0 {
		m["growth_factor"] = float64(recent) / float64(prev)
	}
	m["growth_n"] = float64(recent)
	kids := map[string]int{}
	for id, n := range t.Nodes {
		if !t.Seen[id] {
			continue
		}
		if n.Depth > int(m["max_depth"]) {
			m["max_depth"] = float64(n.Depth)
		}
		if n.Status == "running" {
			m["running_concurrent"]++
		}
		if n.Parent != "" {
			kids[n.Parent]++
			if kids[n.Parent] > int(m["breadth_per_parent"]) {
				m["breadth_per_parent"] = float64(kids[n.Parent])
			}
		}
	}
	for _, e := range t.Usage {
		if d := now.Sub(e.At); d >= 0 && d < time.Minute {
			m["tokens_per_min"] += float64(e.TokensIn + e.TokensOut + e.TokensCache)
		}
	}
	return m
}
