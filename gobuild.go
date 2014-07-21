package pickett

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/igneoussystems/pickett/io"
)

// GoBuildNode has a dependecy between the object to build and the image
// that is used to build it.  This implements the Node interface.
type GoBuildNode struct {
	//in      []Node
	runIn   Node
	out     []Node
	tag     string
	pkgs    []string
	test    bool
	tagTime time.Time
}

// IsOutOfDate returns true if the tag that we are trying to produce is
// before the tag of the image we depend on.
func (b *GoBuildNode) IsOutOfDate(conf *Config, helper io.IOHelper, cli io.DockerCli) (bool, error) {
	t, err := tagToTime(b.tag, cli)
	if err != nil {
		return false, err
	}
	if t.IsZero() {
		fmt.Printf("[pickett] Building %s (tag not found)\n", b.tag)
		return true, nil
	}
	if t.Before(b.runIn.Time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", b.tag, b.runIn.Name())
		return true, nil
	}
	fmt.Printf("[pickett] Faking out of date for %s to force go build/test\n", b.tag)
	return true, nil
}

func (b *GoBuildNode) build(conf *Config, helper io.IOHelper, cli io.DockerCli) error {

	args := []string{}
	if conf.CodeVolume.Directory != "" {
		dir := helper.DirectoryRelative(conf.CodeVolume.Directory)
		args = append(args, "-v", dir+":"+conf.CodeVolume.MountedAt)
	}
	cmd := "install"
	if b.test {
		cmd = "test"
	}
	for _, p := range b.pkgs {
		command := fmt.Sprintf("%s go %s %s", b.runIn.Name(), cmd, p)
		cmdArgs := append(args, strings.Split(command, " ")...)
		printRep := fmt.Sprintf("docker run %s", strings.Trim(fmt.Sprint(cmdArgs), "[]"))
		fmt.Printf("[pickett] %s\n", printRep)
		err := cli.CmdRun(cmdArgs...)
		if err != nil {
			return err
		}
	}
	err := cli.CmdPs("-q", "-l")
	if err != nil {
		return errors.New(fmt.Sprintf("failed trying to ps (%s): %v", b.tag, err))
	}
	id := cli.LastLineOfStdout()
	//command was ok, we need to tag it now
	err = cli.CmdCommit(id, b.tag)
	if err != nil {
		return errors.New(fmt.Sprintf("failed trying to commit (%s): %v", b.tag, err))
	}
	return nil
}

//Build does the work of building a go package in a container. XXX we don't detect
//if go is installed in the container. XXX
func (b *GoBuildNode) Build(conf *Config, helper io.IOHelper, cli io.DockerCli) error {
	helper.Debug("Building (%s) ...", b.Name())
	err := b.BringInboundUpToDate(conf, helper, cli)
	if err != nil {
		return err
	}
	ood, err := b.IsOutOfDate(conf, helper, cli)
	if err != nil {
		return err
	}
	if !ood {
		fmt.Printf("[pickett] %s is up to date.\n", b.tag)
		return nil
	}
	if err := b.build(conf, helper, cli); err != nil {
		return err
	}
	return nil
}

//IsSink is true if this node has no outbound edges.
func (b *GoBuildNode) IsSink() bool {
	return len(b.out) == 0
}

//BringInboundUpToDate walks all the nodes that this node depends on
//up to date.
func (b *GoBuildNode) BringInboundUpToDate(conf *Config, helper io.IOHelper, cli io.DockerCli) error {
	if err := b.runIn.Build(conf, helper, cli); err != nil {
		return err
	}
	return nil
}

//AddOut adds an outgoing edge from this node.
func (b *GoBuildNode) AddOut(n Node) {
	b.out = append(b.out, n)
}

//Name prints the name of this node for a human to consume
func (s *GoBuildNode) Name() string {
	return s.tag
}

//Time returns the most recent build time.
func (s *GoBuildNode) Time() time.Time {
	return s.tagTime
}
