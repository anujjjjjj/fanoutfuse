package claude

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

// Content intentionally omits prompt, text, and tool input apart from selected flags.
type content struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	ID        string `json:"id"`
	ToolUseID string `json:"tool_use_id"`
	AgentID   string `json:"agentId"`
	Input     struct {
		Background bool   `json:"run_in_background"`
		Type       string `json:"subagent_type"`
		Model      string `json:"model"`
	} `json:"input"`
	Content json.RawMessage `json:"content"`
}
type usage struct {
	In          int64 `json:"input_tokens"`
	Out         int64 `json:"output_tokens"`
	CacheRead   int64 `json:"cache_read_input_tokens"`
	CacheCreate int64 `json:"cache_creation_input_tokens"`
}
type Parser struct {
	Intents   map[string]string
	SeenUsage map[string]usage
}

var agentResult = regexp.MustCompile(`agentId[:"\s]+([a-zA-Z0-9_-]+)`)

func (p *Parser) Parse(line []byte, sid, aid string, now time.Time) []model.Event {
	var row struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Agent     string `json:"agentId"`
		Message   struct {
			ID      string          `json:"id"`
			Content json.RawMessage `json:"content"`
			Usage   usage           `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &row) != nil {
		return nil
	}
	at := now
	if v, err := time.Parse(time.RFC3339Nano, row.Timestamp); err == nil {
		at = v
	}
	if p.Intents == nil {
		p.Intents = map[string]string{}
	}
	if p.SeenUsage == nil {
		p.SeenUsage = map[string]usage{}
	}
	var out []model.Event
	var blocks []content
	_ = json.Unmarshal(row.Message.Content, &blocks)
	for _, b := range blocks {
		if b.Type == "tool_use" && (b.Name == "Task" || b.Name == "Agent") {
			p.Intents[b.ID] = aid
			out = append(out, model.Event{Kind: "intent", Session: sid, Agent: aid, At: at, ToolID: b.ID, Type: b.Input.Type, Model: b.Input.Model, Background: b.Input.Background})
		}
		if b.Type == "tool_result" {
			child := b.AgentID
			if child == "" {
				if m := agentResult.FindSubmatch(b.Content); len(m) == 2 {
					child = string(m[1])
				}
			}
			if parent, ok := p.Intents[b.ToolUseID]; ok && child != "" {
				out = append(out, model.Event{Kind: "attribute", Session: sid, Agent: child, Parent: parent, At: at})
			}
		}
	}
	u := row.Message.Usage
	key := sid + ":" + aid + ":" + row.Message.ID
	old := p.SeenUsage[key]
	if row.Message.ID == "" {
		old = usage{}
	}
	delta := func(n, o int64) int64 {
		if n > o {
			return n - o
		}
		return 0
	}
	in, output, cache := delta(u.In, old.In), delta(u.Out, old.Out), delta(u.CacheRead+u.CacheCreate, old.CacheRead+old.CacheCreate)
	if in+output+cache > 0 {
		out = append(out, model.Event{Kind: "usage", Session: sid, Agent: aid, At: at, TokensIn: in, TokensOut: output, TokensCache: cache})
		p.SeenUsage[key] = u
	}
	if row.Type == "agent_end" || row.Type == "agent_stop" {
		out = append(out, model.Event{Kind: "end", Session: sid, Agent: aid, At: at})
	}
	return out
}
