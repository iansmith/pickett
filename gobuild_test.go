package pickett

import (
	"errors"
	"strings"
	"testing"
	"time"

	"code.google.com/p/gomock/gomock"
	docker_utils "github.com/docker/docker/utils"

	"github.com/igneous-systems/pickett/io"
)

func setupForDontBuildBletch(controller *gomock.Controller, helper *io.MockHelper, cli *io.MockDockerCli) *Config {
	setupForExample1Conf(controller, helper)
	//ignoring error is ok because tested in TestConf
	c, _ := NewConfig(strings.NewReader(example1), helper, cli)

	//fake out the building of bletch
	now := time.Now()
	hourAgo := now.Add(-1 * time.Hour)
	helper.EXPECT().LastTimeInDirRelative("mydir").Return(hourAgo, nil)
	insp := io.NewMockInspected(controller)
	insp.EXPECT().CreatedTime().Return(now)
	cli.EXPECT().DecodeInspect("blah/bletch").Return(insp, nil)

	return c
}

func TestGoPackagesFailOnBuildStep2(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)
	vbox := io.NewMockVirtualBox(controller)

	c := setupForDontBuildBletch(controller, helper, cli)

	//this is called to figure out how to mount the source... we don't care how many
	//times this happens
	vbox.EXPECT().NeedPathTranslation().Return(false).AnyTimes()
	helper.EXPECT().DirectoryRelative("src").Return("/home/gredo/src").AnyTimes()

	//we want to start a build of "chattanooga"
	fakeInspectError := &docker_utils.StatusError{
		StatusCode: 1,
	}
	cli.EXPECT().DecodeInspect("chattanooga").Return(nil, fakeInspectError)

	// mock out the docker api calls to build the software
	first := cli.EXPECT().CmdRun(false, "-v", "/home/gredo/src:/han", "blah/bletch", "go", "install",
		"p4...")
	fakeErr := errors.New("whoa doggie")
	cli.EXPECT().CmdRun(false, "-v", "/home/gredo/src:/han", "blah/bletch", "go", "install",
		"p5/p6").Return(fakeErr).After(first)

	//the code will dump the error output on a build error
	cli.EXPECT().DumpErrOutput()

	if err := c.Initiate("chattanooga", helper, cli, etcd, vbox); err != fakeErr {
		t.Errorf("failed to get expected error: %v", err)
	}
}

func TestGoPackagesAllBuilt(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)
	vbox := io.NewMockVirtualBox(controller)

	c := setupForDontBuildBletch(controller, helper, cli)

	//this is called to figure out how to mount the source... we don't care how many
	//times this happens
	vbox.EXPECT().NeedPathTranslation().Return(false).AnyTimes()
	helper.EXPECT().DirectoryRelative("src").Return("/home/gredo/src").AnyTimes()

	//we want to start a build of "nashville" when we are first asked, but the second
	//time we want to give the current time
	fakeInspectError := &docker_utils.StatusError{
		StatusCode: 1,
	}
	now := time.Now()
	insp := io.NewMockInspected(controller)
	insp.EXPECT().CreatedTime().Return(now)
	first := cli.EXPECT().DecodeInspect("nashville").Return(nil, fakeInspectError)
	cli.EXPECT().DecodeInspect("nashville").Return(insp, nil).After(first)

	// test we are already sure we need to build, so we don't test to see if OOD
	// via go, just run the build
	cli.EXPECT().CmdRun(false, "-v", "/home/gredo/src:/han", "blah/bletch", "go", "test",
		"p1...")
	cli.EXPECT().CmdRun(false, "-v", "/home/gredo/src:/han", "blah/bletch", "go", "test",
		"p2/p3")

	//after we build successfully, we use "ps -q -l" to check to see the id of
	//the container that we built in
	expectContainerPSAndCommit(cli)

	//hit it!
	c.Initiate("nashville", helper, cli, etcd, vbox)
}

func expectContainerPSAndCommit(cli *io.MockDockerCli) {
	cli.EXPECT().CmdPs("-q", "-l").Return(nil)
	fakeContainer := "1234ffee5678"
	cli.EXPECT().LastLineOfStdout().Return(fakeContainer)
	cli.EXPECT().CmdCommit(fakeContainer, "nashville")
}

func TestGoPackagesOODOnSource(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)
	vbox := io.NewMockVirtualBox(controller)

	c := setupForDontBuildBletch(controller, helper, cli)

	//this is called to figure out how to mount the source... we don't care how many
	//times this happens
	vbox.EXPECT().NeedPathTranslation().Return(true).AnyTimes()
	vbox.EXPECT().CodeVolumeToVboxPath("/home/gredo/project/bounty/src").Return("/vagrant/src", nil).AnyTimes()
	helper.EXPECT().DirectoryRelative("src").Return("/home/gredo/project/bounty/src").AnyTimes()

	//we want to suggest that the nashville container was built recently when first
	//asked... we will be asked a second time, but it gets discarded so we just
	//do the same thing both times
	now := time.Now()
	insp := io.NewMockInspected(controller)
	insp.EXPECT().CreatedTime().Return(now).Times(2)
	cli.EXPECT().DecodeInspect("nashville").Return(insp, nil).Times(2)

	//
	// this is the test of how the go source OOD really works
	//

	// test for code build needed, then build it
	cli.EXPECT().CmdRun(false, "-v", "/vagrant/src:/han", "blah/bletch", "go", "test",
		"-n", "p1...")
	cli.EXPECT().CmdRun(false, "-v", "/vagrant/src:/han", "blah/bletch", "go", "test",
		"p1...")

	//test for code build needed, then build it
	cli.EXPECT().CmdRun(false, "-v", "/vagrant/src:/han", "blah/bletch", "go", "test",
		"-n", "p2/p3")
	cli.EXPECT().CmdRun(false, "-v", "/vagrant/src:/han", "blah/bletch", "go", "test",
		"p2/p3")

	//
	//Fake the results of the two "probes" with -n, we want to return true (meaning that
	//there is no output, thus to code is up to date) on the first one.  we return false
	//on the second one and it needs to be the second one because the system won't
	//bother asking about the second one if the first one already means we are OOD
	//
	first := cli.EXPECT().EmptyOutput().Return(true)
	cli.EXPECT().EmptyOutput().Return(false).After(first)

	//after we build successfully, we use "ps -q -l" to check to see the id of
	//the container that we built in.
	expectContainerPSAndCommit(cli)

	//hit it!
	c.Initiate("nashville", helper, cli, etcd, vbox)

}

func TestVbox(T *testing.T) {
	vbox, err := io.NewVirtualBox(true)
	if err != nil {
		T.Fatalf("%v", err)
	}
	_, err = vbox.CodeVolumeToVboxPath("/home/iansmith/foo")
	if err != nil {
		T.Fatalf("%v", err)
	}
}
