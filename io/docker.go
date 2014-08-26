package io

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
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

type apiContainers []docker.APIContainers

type apiImages []docker.APIImages

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
	Devices    map[string]string
	Links      map[string]string
	Privileged bool
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

type CopyArtifact struct {
	SourcePath, DestinationDir string
}

type DockerCli interface {
	CmdRun(*RunConfig, ...string) (*bytes.Buffer, string, error)
	CmdTag(string, bool, *TagInfo) error
	CmdCommit(string, *TagInfo) (string, error)
	CmdRetrieve(*tar.Writer, *bytes.Buffer, []*CopyArtifact, string) error
	CmdBuildFromTarball(*BuildConfig, []byte, string) error
	CmdLastModTime(map[string]string, string, []*CopyArtifact) (time.Time, error)
	CmdStop(string) error
	CmdRmContainer(string) error
	CmdRmImage(string) error
	InspectImage(string) (InspectedImage, error)
	InspectContainer(string) (InspectedContainer, error)
	ListContainers() (apiContainers, error)
	ListImages() (apiImages, error)
}

type InspectedImage interface {
	CreatedTime() time.Time
	ID() string
	ContainerID() string
}

type InspectedContainer interface {
	Running() bool
	CreatedTime() time.Time
	ContainerName() string
	ContainerID() string
	ExitStatus() int
	Ip() string
	Ports() []string
}

//NewDocker returns a connection to the docker server.  Pickett assumes that
//the DockerCli is "passed in from the outside".
func NewDockerCli() (DockerCli, error) {
	if err := validateDockerHost(); err != nil {
		return nil, err
	}
	return newDockerCli()
}

type dockerCli struct {
	client *docker.Client
}

// newDockerCli builds a new docker interface and returns it. It
// assumes that the DOCKER_HOST env var has already been
// validated.
func newDockerCli() (DockerCli, error) {
	result := &dockerCli{}
	var err error
	result.client, err = docker.NewClient(os.Getenv("DOCKER_HOST"))
	if err != nil {
		return nil, err
	}
	flog.Debugf("[docker cmd] export DOCKER_HOST='%s'", os.Getenv("DOCKER_HOST"))
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
		flog.Debugf("[docker cmd] Attempting to create container %s (%d) from image: %s", opts.Name, tries, opts.Config.Image)

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
		opts.Name = newPhrase()
		flog.Debugf("[docker cmd] Creating container named: %s", opts.Name)

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

	fordebug := new(bytes.Buffer)
	cont, err := d.createNamedContainer(config)
	if err != nil {
		return nil, "", err
	}
	fordebug.WriteString(fmt.Sprintf("docker run %v ", cont.Name))
	host := &docker.HostConfig{}

	//flatten links for consumption by go-dockerclient
	flatLinks := []string{}
	for k, v := range runconf.Links {
		flatLinks = append(flatLinks, fmt.Sprintf("%s:%s", k, v))
		fordebug.WriteString(fmt.Sprintf("--link %s:%s ", k, v))
	}
	host.Links = flatLinks
	host.Binds = []string{}
	for k, v := range runconf.Volumes {
		host.Binds = append(host.Binds, fmt.Sprintf("%s:%s", k, v))
		fordebug.WriteString(fmt.Sprintf("-v %s:%s ", k, v))
	}

	for k, v := range runconf.Devices {
		if len(s) >= 2 {
			instance, _ := strconv.Atoi(s[2])
			letter := 'b' + instance
			k = strings.Replace(k, "?", string(letter), -1)
		}
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
			fordebug.WriteString(fmt.Sprintf("-p %s:%s:%s ", m.HostIp, m.HostPort, m.HostPort))
		}
	}
	host.PortBindings = convertedMap

	host.Privileged = runconf.Privileged

	flog.Debugf("[docker cmd] %s%s", fordebug.Bytes(), strings.Join(config.Cmd, " "))

	err = d.client.StartContainer(cont.ID, host)
	if err != nil {
		return nil, "", err
	}

	if runconf.Attach {
		//These are the right settings if you want to "watch" the output of the command and wait for
		//it to terminate

		err = d.client.AttachToContainer(docker.AttachToContainerOptions{
			Container:    cont.ID,
			InputStream:  os.Stdin,
			OutputStream: os.Stdout,
			ErrorStream:  os.Stderr,
			Logs:         true,
			Stdin:        true,
			Stdout:       true,
			Stderr:       true,
			Stream:       true,
		})

		if err != nil {
			return nil, "", err
		}

		// There's a docker bug where Attach prematurely exits.
		// To prevent that we DO allow an attach to also wait for
		// the container to exit.
		if runconf.WaitOutput {
			status, err := d.client.WaitContainer(cont.ID)
			if err != nil {
				return nil, "", err
			} else if status != 0 {
				return nil, "", fmt.Errorf("Non-zero exitcode %v from %v", status, cont.Name)
			}
		}

		return nil, cont.ID, nil
	} else if runconf.WaitOutput {
		// wait for result and return a buffer with the output

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
	flog.Debugf("Stopping container %s\n", contID)
	return d.client.StopContainer(contID, 2)
}

func (d *dockerCli) CmdRmImage(imgID string) error {
	flog.Debugf("Removing image %s\n", imgID)
	return d.client.RemoveImage(imgID)
}

func (d *dockerCli) CmdRmContainer(contID string) error {
	flog.Debugf("removing container %s\n", contID)
	opts := docker.RemoveContainerOptions{
		ID: contID,
	}
	return d.client.RemoveContainer(opts)
}

func (d *dockerCli) CmdTag(image string, force bool, info *TagInfo) error {

	flog.Debugf("[docker cmd] Tagging image %s as %s:%s\n", image, info.Repository, info.Tag)

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

	flog.Debugf("[docker cmd] Commit of container. Options: Container: %s, Tag: %s, Repo: %s", opts.Container, opts.Tag, opts.Repository)

	image, err := d.client.CommitContainer(opts)
	if err != nil {
		return "", err
	}

	return image.ID, nil
}

//XXX is it safe to use /bin/true?
func (d *dockerCli) makeDummyContainerToGetAtImage(img string) (string, error) {
	cont, err := d.client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image:      img,
			Entrypoint: []string{"/bin/true"},
		},
	})
	if err != nil {
		return "", err
	}
	return cont.ID, nil
}

func (d *dockerCli) CmdLastModTime(realPathSource map[string]string, img string,
	artifacts []*CopyArtifact) (time.Time, error) {
	if len(realPathSource) == len(artifacts) {
		flog.Debugln("no work to do in the container for last mod time, no artifacts inside it.")
		return time.Time{}, nil
	}
	cont, err := d.makeDummyContainerToGetAtImage(img)
	if err != nil {
		return time.Time{}, err
	}
	err = d.client.StartContainer(cont, &docker.HostConfig{})
	if err != nil {
		return time.Time{}, err
	}
	//walk each artifact, getting it from the container, skipping sources
	best := time.Time{}
	for _, a := range artifacts {
		_, found := realPathSource[a.SourcePath]
		if found {
			continue
		}
		//pull it from container
		buf := new(bytes.Buffer)
		flog.Debugf("copying from container %s. Resource %s to %s", cont, a.SourcePath, a.DestinationDir)

		err = d.client.CopyFromContainer(docker.CopyFromContainerOptions{
			OutputStream: buf,
			Container:    cont,
			Resource:     a.SourcePath,
		})
		if err != nil {
			return time.Time{}, err
		}
		//var out bytes.Buffer
		r := bytes.NewReader(buf.Bytes())
		tr := tar.NewReader(r)
		for {
			entry, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return time.Time{}, err
			}
			flog.Debugf("read file from container: %s, %v", entry.Name, entry.ModTime)
			if !entry.FileInfo().IsDir() {
				if entry.ModTime.After(best) {
					best = entry.ModTime
				}
			}
		}
	}
	return best, nil
}

//
// HACK! Poll the server until the image is built.
//
func hacky_poll(d *dockerCli) chan struct{} {
	term := make(chan struct{})
	go func() {
		for {
			d.client.Ping()
			select {
			case <-time.After(300 * time.Millisecond):
			case <-term:
				return
			}
		}
	}()
	return term
}

//CmdRetrieve pulls a collection files into a tarball and writes them onto the
//tar archive provided.  It writes a Dockerfile formatted result on the writer
//provided but it does not write that file to the archive nor does it finish
//writing to the archive (it's still open when this function exits)
func (d *dockerCli) CmdRetrieve(out *tar.Writer, dockerfile *bytes.Buffer,
	artifact []*CopyArtifact, sourceImage string) error {

	cont, err := d.makeDummyContainerToGetAtImage(sourceImage)
	if err != nil {
		return err
	}

	dockerfile.WriteString(fmt.Sprintf("FROM %s\n", sourceImage))

	for _, a := range artifact {
		//pull it from container
		buf := new(bytes.Buffer)
		//this is the docker CP
		err = d.client.CopyFromContainer(docker.CopyFromContainerOptions{
			OutputStream: buf,
			Container:    cont,
			Resource:     a.SourcePath,
		})
		if err != nil {
			return err
		}
		//we are going to read the bytes retreived as a tarball
		r := bytes.NewReader(buf.Bytes())
		tr := tar.NewReader(r)

		//we must loop because the result could be many files if the
		//artifact was a directory
		for {
			entry, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			flog.Debugf("read file from container: %s", entry.Name)
			//we skip the directories because they are not going to be
			//needed in the result
			if !entry.FileInfo().IsDir() {
				dockerfile.WriteString(fmt.Sprintf("COPY %s %s\n", entry.Name, a.DestinationDir+"/"+entry.Name))
				if err := out.WriteHeader(entry); err != nil {
					return err
				}
				if _, err := io.Copy(out, tr); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

//CmdBuildFromTarball is the underlying primitive for constructing a new image.
//This expects the second argument to be a tarball.
func (d *dockerCli) CmdBuildFromTarball(config *BuildConfig, tarball []byte, resultName string) error {
	opts := docker.BuildImageOptions{
		Name:           resultName,
		InputStream:    bytes.NewBuffer(tarball),
		OutputStream:   os.Stdout,
		RmTmpContainer: config.RemoveTemporaryContainer,
		SuppressOutput: false,
		NoCache:        config.NoCache,
	}

	term := hacky_poll(d)
	defer close(term)

	flog.Debugf("[docker cmd] Building image, result name: %s", opts.Name)
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

func (d *dockerCli) ListContainers() (apiContainers, error) {
	containers, err := d.client.ListContainers(docker.ListContainersOptions{All: true})
	return containers, err
}

func (d *dockerCli) ListImages() (apiImages, error) {
	return d.client.ListImages(true)
}

//Wrappers for getting inspections
func (i *imageInspect) CreatedTime() time.Time {
	return i.wrapped.Created
}

func (i *imageInspect) ID() string {
	return i.wrapped.ID
}

func (i *imageInspect) ContainerID() string {
	return i.wrapped.Container
}

func (c *contInspect) Ip() string {
	return c.wrapped.NetworkSettings.IPAddress
}

func (c *contInspect) Ports() []string {
	ports := []string{}
	for k, _ := range c.wrapped.NetworkSettings.Ports {
		ports = append(ports, k.Port())
	}
	return ports
}

func (c *contInspect) CreatedTime() time.Time {
	return c.wrapped.Created
}

func (c *contInspect) Running() bool {
	return c.wrapped.State.Running
}

func (c *contInspect) ContainerName() string {
	return strings.TrimLeft(c.wrapped.Name, "/")
}

func (c *contInspect) ContainerID() string {
	return c.wrapped.ID
}

func (c *contInspect) ExitStatus() int {
	return c.wrapped.State.ExitCode
}
