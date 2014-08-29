package pickett

import (
	"strings"
	"testing"
	"time"

	"code.google.com/p/gomock/gomock"
	"github.com/igneous-systems/pickett/io"
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
			"RunIn": "netexample:part1",
			"Packages": [
				"mypackage1",
				"mypackage2"
			]
		}
	],
	"Topologies" : {
		"someothergraph" : [
			{
				"Name": "part4",
				"RunIn": "part4-image",
				"EntryPoint": ["/bin/part4.sh"],
				"Policy": "Continue"
			},
			{
				"Name": "part3",
				"RunIn": "part3-image",
				"EntryPoint": ["/bin/part3-start.sh"],
				"Policy": "Always",
				"Instances": 2,
				"Consumes": ["part4"]
			}
		]
	}
}
`

func TestMultipleInstances(T *testing.T) {
	controller := gomock.NewController(T)
	defer controller.Finish()

	helper := io.NewMockHelper(controller)
	cli := io.NewMockDockerCli(controller)
	etcd := io.NewMockEtcdClient(controller)

	ignoredInspect := io.NewMockInspectedImage(controller)

	//time info
	now := time.Now()
	oneHrAgo := now.Add(-1 * time.Hour)
	oneMinAgo := now.Add(-1 * time.Minute)
	oneHrAgoOneMin := oneHrAgo.Add(-1 * time.Minute)

	PART3KEY0 := "/pickett" + "/" + CONTAINERS + "/" + "someothergraph" + "/" + "part3" + "/" + "0"
	PART3KEY1 := "/pickett" + "/" + CONTAINERS + "/" + "someothergraph" + "/" + "part3" + "/" + "1"
	PART4 := "/pickett" + "/" + CONTAINERS + "/" + "someothergraph" + "/" + "part4" + "/" + "0"

	//called as part of config check
	helper.EXPECT().OpenDockerfileRelative("somedir").Return(nil, nil)
	helper.EXPECT().LastTimeInDirRelative("somedir").Return(oneHrAgoOneMin, nil).AnyTimes() //why?

	//image name for these is checked in the config parsing, we act as though they exists
	cli.EXPECT().InspectImage("part3-image").Return(ignoredInspect, nil)
	cli.EXPECT().InspectImage("part4-image").Return(ignoredInspect, nil)

	//it's going to try to get the instances that already exist for part3, we return as if
	//there were none
	etcd.EXPECT().Get(PART3KEY0).Return("", false, nil)
	etcd.EXPECT().Get(PART3KEY1).Return("", false, nil)

	//pass
	cli.EXPECT().CmdRun(gomock.Any(), "/bin/part3-start.sh", "someothergraph", "0").Return(nil, "p3cont0", nil)
	cli.EXPECT().CmdRun(gomock.Any(), "/bin/part3-start.sh", "someothergraph", "1").Return(nil, "p3cont1", nil)

	//testing to see if part one improved is up, we act like its still up, note it is checked
	//twice, one for each instance of part3
	HENDRIX := "merdered_hendrix"
	hendrixCont := io.NewMockInspectedContainer(controller)
	etcd.EXPECT().Get(PART4).Return(HENDRIX, true, nil).Times(2)
	cli.EXPECT().InspectContainer(HENDRIX).Return(hendrixCont, nil).Times(2)
	hendrixCont.EXPECT().Running().Return(true).Times(2)
	hendrixCont.EXPECT().CreatedTime().Return(oneMinAgo).Times(2)

	//we need to handle the queries about part3
	IP0 := "0.1.2.3"
	IP1 := "1.2.3.4"
	PORT0 := "1023"
	PORT1 := "1022"
	vanZant0 := io.NewMockInspectedContainer(controller)
	vanZant0.EXPECT().ContainerName().Return("rvanzant0").Times(2)
	vanZant0.EXPECT().Ip().Return(IP0)
	vanZant0.EXPECT().Ports().Return([]string{PORT0})
	vanZant1 := io.NewMockInspectedContainer(controller)
	vanZant1.EXPECT().ContainerName().Return("rvanzant1").Times(2)
	vanZant1.EXPECT().Ip().Return(IP1)
	vanZant1.EXPECT().Ports().Return([]string{PORT1})

	cli.EXPECT().InspectContainer("p3cont0").Return(vanZant0, nil)
	cli.EXPECT().InspectContainer("p3cont1").Return(vanZant1, nil)

	etcd.EXPECT().Put("/pickett/containers/someothergraph/part3/0", "rvanzant0").Return("ignored0", nil)
	etcd.EXPECT().Put("/pickett/containers/someothergraph/part3/1", "rvanzant1").Return("ignored1", nil)
	etcd.EXPECT().Put("/pickett/ips/someothergraph/part3/0", IP0).Return("ignored-ip0", nil)
	etcd.EXPECT().Put("/pickett/ips/someothergraph/part3/1", IP1).Return("ignored-ip1", nil)
	etcd.EXPECT().Put("/pickett/ports/someothergraph/part3/0", PORT0).Return("ignored-port0", nil)
	etcd.EXPECT().Put("/pickett/ports/someothergraph/part3/1", PORT1).Return("ignored-port1", nil)

	c, err := NewConfig(strings.NewReader(netExample), helper, cli, etcd)
	if err != nil {
		T.Fatalf("can't parse legal config file: %v", err)
	}

	if info := c.nameToTopology["someothergraph"]; info["part3"].instances != 2 {
		T.Fatalf("wrong number of instances, bad parse, on node part3 (%d but expected 2)", info["part3"].instances)
	}

	//do the go build wich consumes the thing built at after-part1
	if _, err := c.Execute("someothergraph.part3", nil); err != nil {
		T.Fatalf("error in Build: %v", err)
	}

}
