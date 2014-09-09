package pickett

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"code.google.com/p/gomock/gomock"

	"github.com/igneous-systems/pickett/io"
)

func setupForDontBuildBletch(controller *gomock.Controller, helper *io.MockHelper,
	cli *io.MockDockerCli, etcd *io.MockEtcdClient) *Config {
	setupForExample1Conf(controller, helper)
	//ignoring error is ok because tested in TestConf
	c, _ := NewConfig(strings.NewReader(example1), helper, cli, etcd)

	//fake out the building of bletch
	now := time.Now()
	hourAgo := now.Add(-1 * time.Hour)
	helper.EXPECT().LastTimeInDirRelative("mydir").Return(hourAgo, nil)
	insp := io.NewMockInspectedImage(controller)
	insp.EXPECT().CreatedTime().Return(now)
	cli.EXPECT().InspectImage("blah:bletch").Return(insp, nil)

	return c
}

func TestGoPackagesFailOnBuildStep2(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)

	c := setupForDontBuildBletch(controller, helper, cli, etcd)

	//this is called to figure out how to mount the source... we don't care how many
	//times this happens
	helper.EXPECT().DirectoryRelative("src").Return("/home/gredo/src").AnyTimes()

	//we want to start a build of "chattanooga"
	fakeInspectError := fmt.Errorf("no such tag, BOOONG you lose")
	cli.EXPECT().InspectImage("fart:chattanooga").Return(nil, fakeInspectError)

	// mock out the docker api calls to build/test the software
	first := cli.EXPECT().CmdRun(gomock.Any(), nil, "go", "install", "p4...").Return(nil, "some_cont", nil)
	fakeErr := errors.New("whoa doggie")
	cli.EXPECT().CmdRun(gomock.Any(), nil, "go", "install", "p5/p6").Return(nil, "", fakeErr).After(first)

	//one commits, one for each successful build
	cli.EXPECT().CmdCommit("some_cont", nil)

	if err := c.Build("fart:chattanooga"); err != fakeErr {
		t.Errorf("failed to get expected error: %v", err)
	}
}

func TestGoPackagesAllBuilt(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)

	c := setupForDontBuildBletch(controller, helper, cli, etcd)

	//this is called to figure out how to mount the source... we don't care how many
	//times this happens
	helper.EXPECT().DirectoryRelative("src").Return("/home/gredo/src").AnyTimes()

	//we want to start a build of "nashville" when we are first asked, but the second
	//time we want to give the current time
	fakeInspectError := fmt.Errorf("no such tag, you loser")
	now := time.Now()
	insp := io.NewMockInspectedImage(controller)
	insp.EXPECT().CreatedTime().Return(now)

	first := cli.EXPECT().InspectImage("test:nashville").Return(nil, fakeInspectError)
	cli.EXPECT().InspectImage("test:nashville").Return(insp, nil).After(first)

	// test we are already sure we need to build, so we don't test to see if OOD
	// via go, just run the build
	cli.EXPECT().CmdRun(gomock.Any(), nil, "go", "test", "p1...").Return(nil, "bah", nil)
	cli.EXPECT().CmdRun(gomock.Any(), nil, "go", "test", "p2/p3").Return(nil, "humbug", nil)

	cli.EXPECT().CmdCommit("bah", nil)
	cli.EXPECT().CmdCommit("humbug", nil).Return("imagehumbug", nil)
	cli.EXPECT().CmdTag("imagehumbug", true, &io.TagInfo{"test", "nashville"})

	//hit it!
	c.Build("test:nashville")
}

func TestGoPackagesOODOnSource(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)

	c := setupForDontBuildBletch(controller, helper, cli, etcd)

	//this is called to figure out how to mount the source... we don't care how many
	//times this happens
	helper.EXPECT().DirectoryRelative("src").Return("/home/gredo/project/bounty/src").AnyTimes()

	//we want to suggest that the nashville container was built recently when first
	//asked... we will be asked a second time, but it gets discarded so we just
	//do the same thing both times
	now := time.Now()
	insp := io.NewMockInspectedImage(controller)
	insp.EXPECT().CreatedTime().Return(now).Times(2)
	cli.EXPECT().InspectImage("test:nashville").Return(insp, nil).Times(2)

	//
	// this is the test of how the go source OOD really works
	//

	// test for code build needed, then build it
	runConfigBase := io.RunConfig{
		Image: "blah/bletch",
		Volumes: map[string]string{
			"/vagrant/src": "/han",
		},
	}
	probe := runConfigBase
	probe.Attach = false

	build := runConfigBase
	build.Attach = true

	buffer := new(bytes.Buffer)
	buffer.WriteString("stuff")

	cli.EXPECT().CmdRun(gomock.Any(), nil, "go", "install", "-n", "p1...").Return(new(bytes.Buffer), "probe1", nil)
	cli.EXPECT().CmdRun(gomock.Any(), nil, "go", "test", "p1...").Return(nil, "cont1", nil)

	//test for code build needed, then build it
	cli.EXPECT().CmdRun(gomock.Any(), nil, "go", "install", "-n", "p2/p3").Return(buffer, "probe2", nil)
	cli.EXPECT().CmdRun(gomock.Any(), nil, "go", "test", "p2/p3").Return(nil, "cont2", nil)

	//removing the probe containers
	cli.EXPECT().CmdRmContainer("probe1").Return(nil)
	cli.EXPECT().CmdRmContainer("probe2").Return(nil)

	//
	//Fake the results of the two "probes" with -n, we want to return true (meaning that
	//there is no output, thus to code is up to date) on the first one.  we return false
	//on the second one and it needs to be the second one because the system won't
	//bother asking about the second one if the first one already means we are OOD
	//
	//first := cli.EXPECT().EmptyOutput(true).Return(true)
	//cli.EXPECT().EmptyOutput(true).Return(false).After(first)

	//after we build successfully, we use "ps -q -l" to check to see the id of
	//the container that we built in.
	//expectContainerPSAndCommit(cli)
	cli.EXPECT().CmdCommit("cont1", nil).Return("someid", nil)
	cli.EXPECT().CmdCommit("cont2", nil).Return("someotherid", nil)
	cli.EXPECT().CmdTag("someotherid", true, &io.TagInfo{"test", "nashville"})
	//hit it!
	c.Build("test:nashville")

}
