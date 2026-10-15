// Package model defines metadata exchanged by adapters and the detector.
package model

import "time"

type Event struct {
	Kind                                        string
	Session, Agent, Parent, ToolID, Type, Model string
	At                                          time.Time
	Background                                  bool
	TokensIn, TokensOut, TokensCache            int64
}
type Node struct {
	ID                               string `json:"agent_id"`
	Parent                           string `json:"parent_id"`
	Depth                            int    `json:"depth"`
	Type, Model                      string
	Background                       bool
	Created, Last                    time.Time
	SpawnCalls, Bytes                int64
	TokensIn, TokensOut, TokensCache int64
	Status                           string
}
type Verdict struct {
	Level, Rule      string
	Value, Threshold float64
	At               time.Time
}
type Action struct {
	Step            string    `json:"step"`
	At              time.Time `json:"at"`
	OK              bool      `json:"ok"`
	Error           string    `json:"err,omitempty"`
	SpawnsAfterTrip int       `json:"spawns_after_trip"`
}
type Incident struct {
	ID, Platform, Session, Project, Version string
	Started, Warned, Tripped, Contained     time.Time
	Rule                                    string
	Value, Threshold                        float64
	Actions                                 []Action
	Total, After, MaxDepth                  int
	TokensIn, TokensOut, TokensCache        int64
	Cost                                    float64
	Projected                               int
	Resolution                              string
	Nodes                                   []Node
	Buckets                                 map[int64]int
}
type Session struct {
	ID         string `json:"session_id"`
	PID        int    `json:"pid"`
	CWD        string `json:"cwd"`
	TTY        string `json:"tty"`
	Background bool   `json:"background"`
}
