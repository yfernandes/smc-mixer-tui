package backend

import (
	"context"
	"encoding/json"
)

// TargetID is globally namespaced as "<backend>:<target>".
type TargetID string

type ParamKind uint8

const (
	ParamContinuous ParamKind = iota
	ParamToggle
	ParamTrigger
	ParamComposite
)

type ParamSpec struct {
	ID       string
	Kind     ParamKind
	Readable bool
	Push     bool
}

type TargetInfo struct {
	ID     TargetID
	Label  string
	Params []ParamSpec
	Ext    json.RawMessage
}

type Value struct {
	F float64
	B bool
}

type Backend interface {
	Name() string
	Targets(ctx context.Context) ([]TargetInfo, error)
	Set(ctx context.Context, t TargetID, param string, v Value) error
	Get(ctx context.Context, t TargetID, param string) (Value, bool, error)
}

type Watcher interface {
	Watch(ctx context.Context, ch chan<- []TargetInfo)
}

type Viewer interface {
	View(ctx context.Context, view string, req json.RawMessage) (json.RawMessage, error)
}
