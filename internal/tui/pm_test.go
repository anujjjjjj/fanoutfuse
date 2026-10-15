package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

func TestMarkdownTablesAndRanking(t *testing.T) {
	in := &model.Incident{ID: "test", Project: "/work/api|service\nnext", MaxDepth: 2, Nodes: []model.Node{
		{ID: "a", TokensIn: 100}, {ID: "b", TokensIn: 100}, {ID: "c", Parent: "b", TokensIn: 1},
		{ID: "d", TokensIn: 200}, {ID: "e"}, {ID: "f"},
	}, Actions: []model.Action{{Step: "contain", OK: false, Error: "error|detail\nnext"}}}
	var out bytes.Buffer
	if err := Render(&out, in, true); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"# FanoutFuse post-mortem · /work/api&#124;service next", "| Key facts | Value |", "| Depth | Agents |", "| Agent | Subagent type | Tokens | Kids |", "| Step | At | Status | Spawns after trip |", "error&#124;detail next"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q: %s", want, s)
		}
	}
	if strings.Index(s, "| d |") > strings.Index(s, "| b |") || strings.Index(s, "| b |") > strings.Index(s, "| a |") {
		t.Fatal("incorrect ranking", s)
	}
	if strings.Contains(s, "| f |") {
		t.Fatal("more than five branches")
	}
	for _, md := range []bool{false, true} {
		out.Reset()
		in.Project = ""
		if err := Render(&out, in, md); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Split(out.String(), "\n")[0], " · -") {
			t.Fatal("missing project placeholder", out.String())
		}
	}
}

func TestLocalTimestamps(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("IST", 19800)
	defer func() { time.Local = old }()
	at := time.Date(2026, 10, 15, 12, 47, 17, 0, time.UTC)
	in := &model.Incident{Tripped: at, Contained: at, Started: at, Value: 2, Threshold: 2, Actions: []model.Action{{At: at, OK: true}}}
	for _, md := range []bool{false, true} {
		var out bytes.Buffer
		if err := Render(&out, in, md); err != nil {
			t.Fatal(err)
		}
		if strings.Count(out.String(), "2026-10-15 18:17:17 IST") != 3 {
			t.Fatal(out.String())
		}
		comparison := "2 >= 2"
		if md {
			comparison = "2 &gt;= 2"
		}
		if !strings.Contains(out.String(), comparison) {
			t.Fatal(out.String())
		}
	}
}
