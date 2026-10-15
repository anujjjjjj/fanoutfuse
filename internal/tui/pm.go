// Package tui renders a compact, metadata-only incident report.
package tui

import (
	"fmt"
	"github.com/anujjjjjj/fanoutfuse/internal/model"
	"io"
	"sort"
	"strings"
	"time"
)

func Sparkline(b map[int64]int) string {
	if len(b) == 0 {
		return ""
	}
	keys := make([]int64, 0, len(b))
	max := 1
	for k, v := range b {
		keys = append(keys, k)
		if v > max {
			max = v
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	lo, hi := keys[0], keys[len(keys)-1]
	width := int((hi-lo)/10000) + 1
	stride := 1
	if width > 60 {
		stride = (width + 59) / 60
	}
	var out strings.Builder
	chars := []rune("▁▂▃▄▅▆▇█")
	for x := lo; x <= hi; x += int64(stride) * 10000 {
		n := 0
		for i := 0; i < stride; i++ {
			n += b[x+int64(i)*10000]
		}
		idx := n * 7 / max
		if idx > 7 {
			idx = 7
		}
		out.WriteRune(chars[idx])
	}
	return out.String()
}
func Render(w io.Writer, in *model.Incident, md bool) error {
	var b strings.Builder
	if md {
		b.WriteString("# FanoutFuse post-mortem\n\n")
	}
	fmt.Fprintf(&b, "FanoutFuse · incident %s · %s %s · %s · session %s\n", in.ID, in.Platform, in.Version, in.Project, in.Session)
	fmt.Fprintf(&b, "TRIPPED %s %g > %g at %s · time-to-trip %s\n", in.Rule, in.Value, in.Threshold, in.Tripped.Format(time.RFC3339), in.Tripped.Sub(in.Started).Round(time.Millisecond))
	contained := "unverified"
	if !in.Contained.IsZero() {
		contained = in.Contained.Format(time.RFC3339)
	}
	fmt.Fprintf(&b, "Spawns %s · total %d (after trip: %d) · depth max %d\n", Sparkline(in.Buckets), in.Total, in.After, in.MaxDepth)
	fmt.Fprintf(&b, "Tokens %d in / %d out / %d cache · ~$%.4f API-equivalent (estimate, price table v1)\n", in.TokensIn, in.TokensOut, in.TokensCache, in.Cost)
	fmt.Fprintf(&b, "Contained %s · Projected if unstopped (+5m): ~%d agents (rate extrapolation)\n", contained, in.Projected)
	hist := map[int]int{}
	kids := map[string]int{}
	for _, n := range in.Nodes {
		hist[n.Depth]++
		if n.Parent != "" {
			kids[n.Parent]++
		}
	}
	b.WriteString("Tree (by depth):")
	for i := 0; i <= in.MaxDepth; i++ {
		fmt.Fprintf(&b, " d%d=%d", i, hist[i])
	}
	b.WriteString("\nTop-5 branches                         tokens   kids\n")
	nodes := append([]model.Node(nil), in.Nodes...)
	sort.Slice(nodes, func(i, j int) bool {
		a, c := nodes[i], nodes[j]
		ta, tc := a.TokensIn+a.TokensOut+a.TokensCache, c.TokensIn+c.TokensOut+c.TokensCache
		if ta == tc {
			if kids[a.ID] == kids[c.ID] {
				return a.ID < c.ID
			}
			return kids[a.ID] > kids[c.ID]
		}
		return ta > tc
	})
	for i, n := range nodes {
		if i == 5 {
			break
		}
		fmt.Fprintf(&b, " %-24s %-12s %8d %6d\n", n.ID, n.Type, n.TokensIn+n.TokensOut+n.TokensCache, kids[n.ID])
	}
	b.WriteString("Actions:\n")
	actions := in.Actions
	if !md && len(actions) > 8 {
		fmt.Fprintf(&b, " ... %d earlier actions (export with --md)\n", len(actions)-8)
		actions = actions[len(actions)-8:]
	}
	for _, a := range actions {
		status := "ok"
		if !a.OK {
			status = a.Error
		}
		fmt.Fprintf(&b, " %s %s: %s · spawns_after_trip=%d\n", a.At.Format("15:04:05.000"), a.Step, status, a.SpawnsAfterTrip)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
