package pickett

import (
	"code.google.com/p/gomock/gomock"
	"fmt"
	"github.com/igneous-systems/pickett/io"
	"strings"
	"testing"
	"time"
)

var netExample = `
// example that uses networking... part1 is consumed by part2 and when
// part2 is done, we want to snapshot part1.  This uses a container 
// called part1-image to prove that backchaining works correctly across
// a run node.
{
	"Containers" : [
		{
			"Repository": "netexample",
			"Tag" : "part1",
			"Directory" : "somedir"
		}
	],
	"Gobuilds" : [
		{
			"Repository":"netexample",
			"Tag": "uses-part1",
			"RunIn": "netexample:after-part1",
			"Packages": [
				"mypackage1",
				"mypackage2"
			]					
		}
	],
	"Networks" : [
		{
			"Name" : "part1",
			"RunIn": "netexample:part1",
			"EntryPoint": ["/bin/part1.sh"],
			"Policy":"Always"
		},
		{
			"Name" : "part2",
			"RunIn": "part2-image",
			"EntryPoint": ["/bin/part2.sh"],
			"Policy":"Always",
			"Consumes": ["part1"],
			"CommitOnExit": 
			{
				"part1":"netexample:after-part1"
			}			
		}
	]
}
`

func TestNetworkNodes(T *testing.T) {
	controller := gomock.NewController(T)
	defer controller.Finish()

	helper := io.NewMockHelper(controller)
	cli := io.NewMockDockerCli(controller)
	vbox := io.NewMockVirtualBox(controller)
	etcd := io.NewMockEtcdClient(controller)

	//time info
	now := time.Now()
	oneHrAgo := now.Add(-1 * time.Hour)
	oneMinAgo := now.Add(-1 * time.Minute)
	oneHrAgoOneMin := oneHrAgo.Add(-1 * time.Minute)

	//
	// SETUP TEST MOCKS
	// THESE ARE MOSTLY IN ORDER OF THEIR CALLS BY THE CODE UNDER TEST
	//

	//ignore the calls to the debug print
	helper.EXPECT().Debug(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	helper.EXPECT().Debug(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	helper.EXPECT().Debug(gomock.Any(), gomock.Any()).AnyTimes()

	//ignore calls to check fatal if the error is nil
	helper.EXPECT().CheckFatal(gomock.Nil(), gomock.Any()).AnyTimes()

	//the caller is just opening this for the error return, he ignores the file
	helper.EXPECT().OpenDockerfileRelative("somedir").Return(nil, nil)

	part2ImageInsp := io.NewMockInspectedImage(controller)

	//the inspected value returned here is ignored
	cli.EXPECT().InspectImage("part2-image").Return(part2ImageInsp, nil)

	//keys that will be looked up in ETCD
	PART1KEY := "/pickett" + "/" + CONTAINERS + "/" + "part1"
	PART2KEY := "/pickett" + "/" + CONTAINERS + "/" + "part2"

	//lookup part1 in etcd
	etcd.EXPECT().Get(PART1KEY).Return("", false, nil)

	//the time on the somedir (files in that directory) is one hour, one min ago
	helper.EXPECT().LastTimeInDirRelative("somedir").Return(oneHrAgoOneMin, nil)
	//the time on the image is slightly less
	part1Insp := io.NewMockInspectedImage(controller)
	part1Insp.EXPECT().CreatedTime().Return(oneHrAgo)
	cli.EXPECT().InspectImage("netexample:part1").Return(part1Insp, nil)

	//we are going to run part1, we ignore the run parameters because we know
	//it will be running in background
	CONT_ID := "fake_cont_id"
	cli.EXPECT().CmdRun(gomock.Any(), "/bin/part1.sh").Return(nil, CONT_ID, nil)

	//the linkage between part2 and part1 means that the network code has too
	//find the name of the container it just started
	JOPLIN := "overdosed_joplin"
	containerInfo := io.NewMockInspectedContainer(controller)

	//this is called twice, once to store in etcd and once to get the name
	//for the "link" parameter to docker run
	containerInfo.EXPECT().ContainerName().Return(JOPLIN).Times(2)

	//ask for the container name from the container ID
	cli.EXPECT().InspectContainer(CONT_ID).Return(containerInfo, nil)

	//it will store the etcd value
	etcd.EXPECT().Put(PART1KEY, JOPLIN)

	//checking to see if the PART2 is already running, we'll fake that it is
	CURTIS := "hanged_curtis"
	etcd.EXPECT().Get(PART2KEY).Return(CURTIS, true, nil)

	//we will act if the CURTIS container is stopped and was started 1 min ago
	curtisInfo := io.NewMockInspectedContainer(controller)
	curtisInfo.EXPECT().CreatedTime().Return(oneMinAgo)
	curtisInfo.EXPECT().Running().Return(false)

	//it will try to get the info about the previous container to see if it's still running
	cli.EXPECT().InspectContainer(CURTIS).Return(curtisInfo, nil)

	//now it will start part2, we will ignore the run parameters
	CONT_ID2 := "some_fake_container_id"
	cli.EXPECT().CmdRun(gomock.Any(), "/bin/part2.sh").Return(nil, CONT_ID2, nil)

	//two different return values, the first one is for the initial test
	//of "does the tag exist at all?" and we say no, it does not.  The second
	//is after the build is completed and we need to mark the node with a time
	//that it was last updated.
	first := cli.EXPECT().InspectImage("netexample:after-part1").Return(nil, fmt.Errorf("fake error! will be interpreted as as a 'tag not found'"))
	//tell it the current time after the "commit"
	afterInfo := io.NewMockInspectedImage(controller)
	afterInfo.EXPECT().CreatedTime().Return(now)
	cli.EXPECT().InspectImage("netexample:after-part1").Return(afterInfo, nil).After(first)

	//checking that the tag is put on the resulting container... the commit id is ignored, so we nil it
	cli.EXPECT().CmdCommit(CURTIS, &io.TagInfo{"netexample", "after-part1"}).Return("ignored", nil)

	//there will be a call to run the go compiler on each package
	cli.EXPECT().CmdRun(gomock.Any(), "go", "install", "mypackage1").Return(nil, "gocont1", nil)
	cli.EXPECT().CmdRun(gomock.Any(), "go", "install", "mypackage2").Return(nil, "gocont2", nil)

	//and then the go results are committed after each build
	cli.EXPECT().CmdCommit("gocont1", gomock.Any()).Return("goimg1", nil)
	cli.EXPECT().CmdCommit("gocont2", gomock.Any()).Return("goimg2", nil)

	//final tag of the output!
	cli.EXPECT().CmdTag("goimg2", true, &io.TagInfo{"netexample", "uses-part1"})

	//we will retreive the tag time but this ends up being discarded
	finalInsp := io.NewMockInspectedImage(controller)
	finalInsp.EXPECT().CreatedTime().Return(now)
	cli.EXPECT().InspectImage("netexample:uses-part1").Return(finalInsp, nil)

	//
	// ACTUAL TEST PART
	//

	c, err := NewConfig(strings.NewReader(netExample), helper, cli, etcd, vbox)
	if err != nil {
		T.Fatalf("can't parse legal config file: %v", err)
	}

	if len(c.nameToNode) != 3 {
		T.Fatalf("wrong nmuber of nodes in the chain")
	}

	if len(c.nameToNetwork) != 2 {
		T.Fatalf("wrong nmuber of nodes in the chain")
	}

	//do the go build wich consumes the thing built at after-part1
	if err := c.Build("netexample:uses-part1"); err != nil {
		T.Fatalf("error in Build: %v", err)
	}
}
