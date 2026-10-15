package tree

import (
	"github.com/anujjjjjj/fanoutfuse/internal/model"
	"testing"
	"time"
)

func TestAttributionDedupAndLateParents(t *testing.T) {
	tr := New()
	at := time.Now()
	e := model.Event{Kind: "spawn", Agent: "child", Parent: "parent", At: at}
	if !tr.Apply(e) || tr.Apply(e) {
		t.Fatal("spawn dedup failed")
	}
	tr.Apply(model.Event{Kind: "spawn", Agent: "parent", Parent: "main", At: at})
	if tr.Nodes["child"].Depth != 2 {
		t.Fatal(tr.Nodes["child"])
	}
	tr.Apply(model.Event{Kind: "attribute", Agent: "parent", Parent: "grandparent", At: at})
	tr.Apply(model.Event{Kind: "spawn", Agent: "grandparent", Parent: "main", At: at})
	if tr.Nodes["child"].Depth != 3 {
		t.Fatal("late ancestor not propagated")
	}
	tr.Apply(model.Event{Kind: "spawn", Agent: "unknown", At: at})
	if tr.Nodes["unknown"].Depth != 1 {
		t.Fatal("unknown depth")
	}
	tr.Apply(model.Event{Kind: "attribute", Agent: "parent", Parent: "child", At: at})
	if tr.Nodes["parent"].Depth > len(tr.Nodes)+1 {
		t.Fatal("cycle did not terminate")
	}
	tr.Apply(model.Event{Kind: "end", Agent: "child", At: at})
	if tr.Metrics(at)["running_concurrent"] != 3 {
		t.Fatal(tr.Metrics(at))
	}
}

func TestSpawnDepthIsAuthoritative(t *testing.T) {
	tr := New()
	depth := 2
	tr.Apply(model.Event{Kind: "spawn", Agent: "child", Parent: "missing", SpawnDepth: &depth})
	tr.Apply(model.Event{Kind: "spawn", Agent: "leaf", Parent: "child"})
	tr.Apply(model.Event{Kind: "attribute", Agent: "missing", Parent: "ancestor"})
	tr.Apply(model.Event{Kind: "attribute", Agent: "main", Type: "worker"})
	if tr.Nodes["child"].Depth != 2 || tr.Nodes["leaf"].Depth != 3 {
		t.Fatalf("explicit depth not preserved: %+v", tr.Snapshot())
	}
	zero := 0
	tr.Apply(model.Event{Kind: "attribute", Agent: "child", SpawnDepth: &zero})
	if tr.Nodes["child"].Depth != 0 || tr.Nodes["leaf"].Depth != 1 {
		t.Fatal("explicit zero depth ignored")
	}
	if tr.Nodes["main"].Type != "main" || tr.Nodes["main"].Depth != 0 {
		t.Fatal("main attribution overwritten")
	}
}
