package pickett

import (
	"time"
)

//Node is the abstraction for an element in the dependency graph.
type Node interface {
	IsOutOfDate(conf *Config, helper IOHelper) (bool, error)
	Build(conf *Config, helper IOHelper) error
	IsSink() bool
	BringInboundUpToDate(conf *Config, helper IOHelper) error
	Name() string
	Time() time.Time
	AddOut(Node) //don't need AddIn because usually constructing it when call this
}
