package io

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	docker "github.com/dotcloud/docker/api/client"
)

//interestingPartsOfInspect is a utility for pulling things out of the json
//returned by docker server inspect function.
type interestingPartsOfInspect struct {
	Created time.Time
	Name    string
	State   interestingPartsOfState
}

type interestingPartsOfState struct {
	Running  bool
	ExitCode int
}

type DockerCli interface {
	CmdRun(bool, ...string) error
	CmdPs(...string) error
	CmdTag(...string) error
	CmdCommit(...string) error
	CmdInspect(...string) error
	CmdBuild(bool, ...string) error
	CmdCp(...string) error
	CmdWait(...string) error
	CmdAttach(...string) error
	CmdStop(...string) error
	Stdout() string
	LastLineOfStdout() string
	Stderr() string
	EmptyOutput() bool
	DecodeInspect(...string) (Inspected, error)
	DumpErrOutput()
}

type Inspected interface {
	CreatedTime() time.Time
	ContainerName() string
	Running() bool
}

//NewDocker returns a connection to the docker server.  Pickett assumes that
//the DockerCli is "passed in from the outside".
func NewDockerCli(debug bool) (DockerCli, error) {
	if err := validateDockerHost(); err != nil {
		return nil, err
	}
	return newDockerCli(debug), nil
}

type dockerCli struct {
	out, err *bytes.Buffer
	cli      *docker.DockerCli
	debug    bool
}

// newDockerCli builds a new docker interface and returns it. It
// assumes that the DOCKER_HOST env var has already been
// validated.
func newDockerCli(debug bool) DockerCli {
	result := &dockerCli{
		out:   new(bytes.Buffer),
		err:   new(bytes.Buffer),
		debug: debug,
	}

	parts := splitProto()
	result.cli = docker.NewDockerCli(nil, result.out, result.err,
		parts[0], parts[1], nil)
	if debug {
		fmt.Printf("[docker cmd] export DOCKER_HOST='%s'\n", os.Getenv("DOCKER_HOST"))
	}
	return result
}

func (d *dockerCli) reset() {
	d.out.Reset()
	d.err.Reset()
}

func (d *dockerCli) newDocker(other io.Writer) *docker.DockerCli {
	parts := splitProto()
	out := io.Writer(os.Stdout)
	if other != nil {
		out = io.MultiWriter(os.Stdout, other)
	}
	return docker.NewDockerCli(os.Stdin, out, os.Stderr, parts[0], parts[1], nil)
}

func (d *dockerCli) CmdRun(teeOutput bool, s ...string) error {
	if teeOutput {
		if d.debug {
			fmt.Printf("[debug] teeing output, so creating new docker CLI instance for stdout, stderr\n")
		}
		return d.newDocker(nil).CmdRun(s...)
	} else {
		return d.caller(d.cli.CmdRun, "run", s...)
	}
}

func (d *dockerCli) CmdPs(s ...string) error {
	return d.caller(d.cli.CmdPs, "ps", s...)
}

func (d *dockerCli) CmdStop(s ...string) error {
	return d.caller(d.cli.CmdStop, "stop", s...)
}

func (d *dockerCli) CmdWait(s ...string) error {
	return d.caller(d.cli.CmdWait, "wait", s...)
}

func (d *dockerCli) CmdTag(s ...string) error {
	return d.caller(d.cli.CmdTag, "tag", s...)
}

func (d *dockerCli) CmdAttach(s ...string) error {
	return d.caller(d.cli.CmdAttach, "attach", s...)
}

func (d *dockerCli) CmdCommit(s ...string) error {
	return d.caller(d.cli.CmdCommit, "commit", s...)
}

func (d *dockerCli) CmdCp(s ...string) error {
	return d.caller(d.cli.CmdCp, "cp", s...)
}

//caller calls the clientAPI of docker on a function of your choice.  This handles the debugging
//output in a standard way so all docker commands and theere output look the same.
func (d *dockerCli) caller(fn func(s ...string) error, name string, s ...string) error {
	d.reset()
	if d.debug {
		fmt.Printf("[docker cmd] docker %s %s\n", name,
			strings.Trim(fmt.Sprint(s), "[]"))
	}
	err := fn(s...)
	if err != nil && d.debug {
		fmt.Printf("[docker result error!] %v\n", err)
		fmt.Printf("[err] %s\n", strings.Trim(d.err.String(), "\n"))
	} else if d.debug {
		if d.out.String() != "" {
			lines := strings.Split(d.out.String(), "\n")
			lines = lines[0 : len(lines)-1] //remove last \n
			if len(lines) == 1 {
				fmt.Printf("[docker result] %s\n", lines[0])
			} else {
				fmt.Printf("[docker result (%d lines)] %s\n",
					len(lines), d.LastLineOfStdout())
			}
		}
	}
	return err
}

//DecodeInspect calls the nispect method of the docker CLI and then decodes the result.
//THis call will fail if the underlying inspect fails.
func (d *dockerCli) DecodeInspect(s ...string) (Inspected, error) {
	err := d.CmdInspect(s...)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(d.out)
	result := []interestingPartsOfInspect{}
	err = dec.Decode(&result)
	if err != nil {
		return nil, err
	}
	if len(result) != 1 {
		return nil, BAD_INSPECT_RESULT
	}
	return &result[0], nil

}

func (d *dockerCli) CmdInspect(s ...string) error {
	return d.caller(d.cli.CmdInspect, "inspect", s...)
}

func (d *dockerCli) CmdBuild(teeOutput bool, s ...string) error {
	if teeOutput {
		if d.debug {
			fmt.Printf("[debug] teeing output to allow build to be seen on stdout, stderr")
		}
		d.out.Reset()
		return d.newDocker(d.out).CmdBuild(s...)
	}
	return d.caller(d.cli.CmdBuild, "build", s...)
}

func (d *dockerCli) Stdout() string {
	return d.out.String()
}

func (d *dockerCli) EmptyOutput() bool {
	return d.out.String() == ""
}

func (d *dockerCli) DumpErrOutput() {
	fmt.Printf("--------------------output----------------------\n")
	fmt.Printf("%s\n", d.out.String())
	fmt.Printf("------------------------------------------------\n")
	fmt.Printf("--------------------error-----------------------\n")
	fmt.Printf("%s\n", d.err.String())
	fmt.Printf("------------------------------------------------\n")
}

func (d *dockerCli) LastLineOfStdout() string {
	s := d.out.String()
	lines := strings.Split(s, "\n")
	//there is a terminating \n on the last line, need to subtract 2 to get
	//the last element of this slice
	if len(lines) < 2 {
		fmt.Printf("panicing due to bad docker result '%s'\n", s)
		panic("badly formed lines of output from docker command")
	}
	return lines[len(lines)-2]
}

func (d *dockerCli) Stderr() string {
	return d.err.String()
}

func (i *interestingPartsOfInspect) CreatedTime() time.Time {
	return time.Time(i.Created)
}

func (i *interestingPartsOfInspect) ContainerName() string {
	return i.Name
}

func (i *interestingPartsOfInspect) Running() bool {
	return i.State.Running
}
