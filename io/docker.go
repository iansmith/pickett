package io

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/fsouza/go-dockerclient"
)

//hide the docker client side types
type imageInspect struct {
	wrapped *docker.Image
}

//hide the docker client side types
type contInspect struct {
	wrapped *docker.Container
}

type Port string
type PortBinding struct {
	HostIp   string
	HostPort string
}

type RunConfig struct {
	Image      string
	Attach     bool
	Volumes    map[string]string
	Ports      map[Port][]PortBinding
	Links      map[string]string
	WaitOutput bool
}

type TagInfo struct {
	Repository string
	Tag        string
}

type BuildConfig struct {
	NoCache                  bool
	RemoveTemporaryContainer bool
}

type DockerCli interface {
	CmdRun(*RunConfig, ...string) (*bytes.Buffer, string, error)
	CmdTag(string, bool, *TagInfo) error
	CmdCommit(string, *TagInfo) (string, error)
	CmdBuild(*BuildConfig, string, string) error
	CmdStop(string) error
	InspectImage(string) (InspectedImage, error)
	InspectContainer(string) (InspectedContainer, error)
}

type InspectedImage interface {
	CreatedTime() time.Time
}

type InspectedContainer interface {
	Running() bool
	CreatedTime() time.Time
	ContainerName() string
	ExitStatus() int
}

//NewDocker returns a connection to the docker server.  Pickett assumes that
//the DockerCli is "passed in from the outside".
func NewDockerCli(debug, showDocker bool) (DockerCli, error) {
	if err := validateDockerHost(); err != nil {
		return nil, err
	}
	return newDockerCli(debug, showDocker)
}

type dockerCli struct {
	client     *docker.Client
	debug      bool
	showDocker bool
}

// newDockerCli builds a new docker interface and returns it. It
// assumes that the DOCKER_HOST env var has already been
// validated.
func newDockerCli(debug, showDocker bool) (DockerCli, error) {
	result := &dockerCli{
		debug:      debug,
		showDocker: showDocker,
	}
	var err error
	result.client, err = docker.NewClient(os.Getenv("DOCKER_HOST"))
	if err != nil {
		return nil, err
	}
	if showDocker {
		fmt.Printf("[docker cmd] export DOCKER_HOST='%s'\n", os.Getenv("DOCKER_HOST"))
	}
	return result, nil
}

func (d *dockerCli) createNamedContainer(config *docker.Config) (*docker.Container, error) {
	tries := 0
	ok := false
	var cont *docker.Container
	var err error
	var opts docker.CreateContainerOptions
	for tries < 3 {
		opts.Config = config
		opts.Name = newPhrase()
		cont, err = d.client.CreateContainer(opts)
		if err != nil {
			detail, ok := err.(*docker.Error)
			if ok && detail.Status == 409 {
				tries++
				continue
			} else {
				return nil, err
			}
		}
		ok = true
		break
	}
	if !ok {
		opts.Name = "" //fallback
		cont, err = d.client.CreateContainer(opts)
		if err != nil {
			return nil, err
		}
	}
	return cont, nil
}

var EMPTY struct{}

func (d *dockerCli) CmdRun(runconf *RunConfig, s ...string) (*bytes.Buffer, string, error) {
	config := &docker.Config{}
	config.Cmd = s
	config.Image = runconf.Image

	cont, err := d.createNamedContainer(config)
	if err != nil {
		return nil, "", err
	}
	host := &docker.HostConfig{}

	//flatten links for consumption by go-dockerclient
	flatLinks := []string{}
	for k, v := range runconf.Links {
		flatLinks = append(flatLinks, fmt.Sprintf("%s:%s", k, v))
	}
	host.Links = flatLinks
	host.Binds = []string{}
	for k, v := range runconf.Volumes {
		host.Binds = append(host.Binds, fmt.Sprintf("%s:%s", k, v))
	}

	//convert the types of the elements of this map so that *our* clients don't
	//see the inner types
	convertedMap := make(map[docker.Port][]docker.PortBinding)
	for k, v := range runconf.Ports {
		key := docker.Port(k)
		convertedMap[key] = []docker.PortBinding{}
		for _, m := range v {
			convertedMap[key] = append(convertedMap[key],
				docker.PortBinding{HostIp: m.HostIp, HostPort: m.HostPort})
		}
	}
	host.PortBindings = convertedMap

	err = d.client.StartContainer(cont.ID, host)
	if err != nil {
		return nil, "", err
	}

	if runconf.Attach {

		if runconf.WaitOutput {
			fmt.Fprintf(os.Stderr, "[pickett warning] shouldn't use WaitOutput with Attach, ignoring.\n")
		}

		//These are the right settings if you want to "watch" the output of the command and wait for
		//it to terminate
		err = d.client.AttachToContainer(docker.AttachToContainerOptions{
			Container:    cont.ID,
			OutputStream: os.Stdout,
			ErrorStream:  os.Stderr,
			Logs:         true,
			Stdout:       true,
			Stderr:       true,
			Stream:       true,
		})

		if err != nil {
			return nil, "", err
		}
		return nil, cont.ID, nil
	}

	//wait for result and return a buffer with the output
	if runconf.WaitOutput {
		_, err = d.client.WaitContainer(cont.ID)
		if err != nil {
			return nil, "", err
		}
		out := new(bytes.Buffer)
		err = d.client.AttachToContainer(docker.AttachToContainerOptions{
			Container:    cont.ID,
			OutputStream: out,
			ErrorStream:  out,
			Logs:         true,
			Stdout:       true,
			Stderr:       true,
		})
		if err != nil {
			return nil, "", err
		}

		return out, cont.ID, nil
	}

	//just start it and return with the id
	return nil, cont.ID, nil
}

func (d *dockerCli) CmdStop(contID string) error {
	fmt.Printf("TRYING TO STOP CONTAINER %s\n", contID)
	return d.client.StopContainer(contID, 10)
}

func (d *dockerCli) CmdTag(image string, force bool, info *TagInfo) error {

	return d.client.TagImage(image, docker.TagImageOptions{
		Force: force,
		Tag:   info.Tag,
		Repo:  info.Repository,
	})
}

func (d *dockerCli) CmdCommit(containerId string, info *TagInfo) (string, error) {
	opts := docker.CommitContainerOptions{
		Container: containerId,
	}
	if info != nil {
		opts.Tag = info.Tag
		opts.Repository = info.Repository
	}
	image, err := d.client.CommitContainer(opts)
	if err != nil {
		return "", err
	}
	return image.ID, nil
}

func (d *dockerCli) CmdBuild(config *BuildConfig, pathToDir string, tag string) error {

	//build tarball
	out := new(bytes.Buffer)
	tw := tar.NewWriter(out)
	dir, err := os.Open(pathToDir)
	if err != nil {
		return err
	}
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("expected %s to be a directory!", dir)
	}
	names, err := dir.Readdirnames(0)
	if err != nil {
		return err
	}
	for _, name := range names {
		path := filepath.Join(pathToDir, name)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name: name,
			Size: info.Size(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		fp, err := os.Open(path)
		if err != nil {
			return err
		}
		content, err := ioutil.ReadAll(fp)
		if err != nil {
			return err
		}
		if _, err := tw.Write(content); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}

	opts := docker.BuildImageOptions{
		Name:           tag,
		InputStream:    bytes.NewBuffer(out.Bytes()),
		OutputStream:   os.Stdout,
		RmTmpContainer: config.RemoveTemporaryContainer,
		SuppressOutput: false,
		NoCache:        config.NoCache,
	}

	if err := d.client.BuildImage(opts); err != nil {
		return err
	}
	return nil
}

func (c *dockerCli) InspectImage(n string) (InspectedImage, error) {
	i, err := c.client.InspectImage(n)
	if err != nil {
		return nil, err
	}
	return &imageInspect{
		wrapped: i,
	}, nil
}

func (c *dockerCli) InspectContainer(n string) (InspectedContainer, error) {
	i, err := c.client.InspectContainer(n)
	if err != nil {
		return nil, err
	}
	return &contInspect{
		wrapped: i,
	}, nil
}

//Wrappers for getting inspections
func (i *imageInspect) CreatedTime() time.Time {
	return i.wrapped.Created
}

func (c *contInspect) CreatedTime() time.Time {
	return c.wrapped.Created
}

func (c *contInspect) Running() bool {
	return c.wrapped.State.Running
}

func (c *contInspect) ContainerName() string {
	return c.wrapped.Name
}

func (c *contInspect) ExitStatus() int {
	return c.wrapped.State.ExitCode
}
