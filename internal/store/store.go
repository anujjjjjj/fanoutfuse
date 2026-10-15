// Package store persists incident metadata in a private SQLite database.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anujjjjjj/fanoutfuse/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS incident (
 id TEXT PRIMARY KEY, platform TEXT NOT NULL, session_id TEXT NOT NULL, project_path TEXT, cli_version TEXT,
 started_at INTEGER, warned_at INTEGER, tripped_at INTEGER, rule TEXT, rule_value REAL, rule_threshold REAL,
 actions TEXT, contained_at INTEGER, spawns_total INTEGER, spawns_after_trip INTEGER, max_depth INTEGER,
 tokens_in INTEGER, tokens_out INTEGER, tokens_cache INTEGER, est_cost_usd REAL,
 projected_spawns_if_unstopped INTEGER, resolution TEXT);
CREATE TABLE IF NOT EXISTS agent_node (
 incident_id TEXT, agent_id TEXT, parent_id TEXT, depth INTEGER, subagent_type TEXT, model TEXT, background INTEGER,
 created_at INTEGER, last_event_at INTEGER, spawn_calls INTEGER, bytes INTEGER, tokens_in INTEGER, tokens_out INTEGER,
 tokens_cache INTEGER, status TEXT, PRIMARY KEY(incident_id,agent_id));
CREATE TABLE IF NOT EXISTS spawn_bucket (incident_id TEXT, bucket_start INTEGER, spawns INTEGER, tokens INTEGER,
 PRIMARY KEY(incident_id,bucket_start));`

func Open(home string) (*Store, error) {
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(home, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(home, "fanoutfuse.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", schema} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
func tm(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v).UTC()
}
func (s *Store) Save(in *model.Incident) error {
	actions, err := json.Marshal(in.Actions)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT OR REPLACE INTO incident VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.ID, in.Platform, in.Session, in.Project, in.Version, ms(in.Started), ms(in.Warned), ms(in.Tripped), in.Rule, in.Value, in.Threshold, string(actions), ms(in.Contained), in.Total, in.After, in.MaxDepth, in.TokensIn, in.TokensOut, in.TokensCache, in.Cost, in.Projected, in.Resolution)
	if err != nil {
		return err
	}
	if err = saveNodes(tx, in.ID, in.Nodes); err != nil {
		return err
	}
	for bucket, count := range in.Buckets {
		if _, err = tx.Exec("INSERT OR REPLACE INTO spawn_bucket VALUES(?,?,?,?)", in.ID, bucket, count, 0); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveSnapshot flushes an active tree without manufacturing an incident.
func (s *Store) SaveSnapshot(session string, nodes []model.Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = saveNodes(tx, "live:"+session, nodes); err != nil {
		return err
	}
	return tx.Commit()
}
func saveNodes(tx *sql.Tx, id string, nodes []model.Node) error {
	if _, err := tx.Exec("DELETE FROM agent_node WHERE incident_id=?", id); err != nil {
		return err
	}
	for _, n := range nodes {
		if _, err := tx.Exec(`INSERT INTO agent_node VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, n.ID, n.Parent, n.Depth, n.Type, n.Model, n.Background, ms(n.Created), ms(n.Last), n.SpawnCalls, n.Bytes, n.TokensIn, n.TokensOut, n.TokensCache, n.Status); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Get(id string) (*model.Incident, error) {
	q := "SELECT * FROM incident WHERE id=?"
	var args []any
	if id == "" {
		q = "SELECT * FROM incident ORDER BY rowid DESC LIMIT 1"
	} else {
		args = append(args, id)
	}
	in := &model.Incident{}
	var a string
	var started, warned, tripped, contained int64
	err := s.db.QueryRow(q, args...).Scan(&in.ID, &in.Platform, &in.Session, &in.Project, &in.Version, &started, &warned, &tripped, &in.Rule, &in.Value, &in.Threshold, &a, &contained, &in.Total, &in.After, &in.MaxDepth, &in.TokensIn, &in.TokensOut, &in.TokensCache, &in.Cost, &in.Projected, &in.Resolution)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no incident found")
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(a), &in.Actions); err != nil {
		return nil, err
	}
	in.Started = tm(started)
	in.Warned = tm(warned)
	in.Tripped = tm(tripped)
	in.Contained = tm(contained)
	rows, err := s.db.Query("SELECT agent_id,parent_id,depth,subagent_type,model,background,created_at,last_event_at,spawn_calls,bytes,tokens_in,tokens_out,tokens_cache,status FROM agent_node WHERE incident_id=?", in.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n model.Node
		var created, last int64
		if err = rows.Scan(&n.ID, &n.Parent, &n.Depth, &n.Type, &n.Model, &n.Background, &created, &last, &n.SpawnCalls, &n.Bytes, &n.TokensIn, &n.TokensOut, &n.TokensCache, &n.Status); err != nil {
			rows.Close()
			return nil, err
		}
		n.Created = tm(created)
		n.Last = tm(last)
		in.Nodes = append(in.Nodes, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.db.Query("SELECT bucket_start,spawns FROM spawn_bucket WHERE incident_id=?", in.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	in.Buckets = map[int64]int{}
	for rows.Next() {
		var b int64
		var n int
		if err = rows.Scan(&b, &n); err != nil {
			return nil, err
		}
		in.Buckets[b] = n
	}
	return in, rows.Err()
}
