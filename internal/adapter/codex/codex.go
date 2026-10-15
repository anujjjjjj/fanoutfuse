// Package codex reserves the v1.1 adapter boundary. No log format is assumed.
package codex

import (
	"context"
	"errors"
	"github.com/anujjjjjj/fanoutfuse/internal/model"
)

var ErrUnsupported = errors.New("codex adapter is not implemented in v1")

type Adapter interface {
	Watch(context.Context, chan<- model.Event) error
}
type Stub struct{}

func (Stub) Watch(context.Context, chan<- model.Event) error { return ErrUnsupported }
