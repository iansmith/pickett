package pickett

import (
	"time"

	"github.com/igneoussystems/pickett/io"
)

//Node is the abstraction for an element in the dependency graph.
type Node interface {
	IsOutOfDate(conf *Config, helper io.IOHelper, cli io.DockerCli) (bool, error)
	Build(conf *Config, helper io.IOHelper, cli io.DockerCli) error
	IsSink() bool
	BringInboundUpToDate(conf *Config, helper io.IOHelper, cli io.DockerCli) error
	Name() string
	Time() time.Time
	AddOut(Node) //don't need AddIn because usually constructing it when call this
}
