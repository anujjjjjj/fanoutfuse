// Package ctl provides a private newline-delimited JSON-RPC Unix control socket.
package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type Request struct {
	JSONRPC string   `json:"jsonrpc"`
	ID      int      `json:"id"`
	Method  string   `json:"method"`
	Params  []string `json:"params,omitempty"`
}
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      int       `json:"id"`
	Result  string    `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type Call struct {
	Request Request
	Reply   chan Response
}

func Listen(home string) (net.Listener, error) {
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	p := filepath.Join(home, "ctl.sock")
	if _, err := os.Stat(p); err == nil {
		c, e := net.DialTimeout("unix", p, 200*time.Millisecond)
		if e == nil {
			c.Close()
			return nil, fmt.Errorf("daemon already running")
		}
		if err = os.Remove(p); err != nil {
			return nil, err
		}
	}
	l, err := net.Listen("unix", p)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(p, 0600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}
func Serve(ctx context.Context, l net.Listener, calls chan<- Call) error {
	go func() { <-ctx.Done(); l.Close() }()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func() {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(15 * time.Second))
			var r Request
			if json.NewDecoder(c).Decode(&r) != nil {
				return
			}
			call := Call{Request: r, Reply: make(chan Response, 1)}
			select {
			case calls <- call:
			case <-ctx.Done():
				return
			}
			select {
			case res := <-call.Reply:
				_ = json.NewEncoder(c).Encode(res)
			case <-ctx.Done():
			}
		}()
	}
}
func Client(home, method string, params ...string) (string, error) {
	c, err := net.DialTimeout("unix", filepath.Join(home, "ctl.sock"), time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if err = json.NewEncoder(c).Encode(Request{JSONRPC: "2.0", ID: 1, Method: method, Params: params}); err != nil {
		return "", err
	}
	var r Response
	if err = json.NewDecoder(c).Decode(&r); err != nil {
		return "", err
	}
	if r.Error != nil {
		return "", fmt.Errorf("%s", r.Error.Message)
	}
	return r.Result, nil
}
