package io

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var (
	PATH_BUSTED            = errors.New("'vboxmanage' is not in your path. It is probably located where VirtualBox is installed.")
	TOO_MANY_VMS           = errors.New("'vboxmanage list runningvms' shows more than 1, or 0, running VMs.  Pickett expects there to be exactly one running VM.")
	CANT_UNDERSTAND_OUTPUT = errors.New("'vboxmanage list runningvms' produced output we can't understand. Expected something like '\"iansmith_default_1406075455778_62045\" {2a2b68ce-7e9a-43f6-8347-d221b79c4661}'. Maybe curly braces in your name?")
)

type VirtualBox interface {
	CodeVolumeToVboxPath(string) (string, error)
	NeedPathTranslation() bool
}

type vboxManage struct {
	debug      bool
	vboxmanage string
}

func NewVirtualBox(debug bool) (VirtualBox, error) {
	p, err := exec.LookPath("vboxmanage")
	if err != nil {
		return nil, PATH_BUSTED
	}
	return &vboxManage{
		debug:      debug,
		vboxmanage: p,
	}, nil
}

//NeedPathTranslation returns true if you are talking to
//a machine across a tcp connection.
func (v *vboxManage) NeedPathTranslation() bool {
	parts := splitProto()
	if strings.HasPrefix(parts[0], "unix") {
		return false
	}
	if !strings.HasPrefix(parts[0], "tcp") {
		fmt.Fprintf(os.Stderr, "warning: unexpected protocol in DOCKER_HOST, not doing path translation\n")
		return false
	}
	return true //tcp
}

//CodeVolumeToVboxPath does the work to figure out for given HOST path
//how to compute a VM path.  It assume that shared folders (in the virtualbox
//sense) are mounted at /, which might be wrong for non-vagrant.
func (v *vboxManage) CodeVolumeToVboxPath(vol string) (string, error) {
	cmd := exec.Command(v.vboxmanage, "list", "runningvms")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	str := strings.Trim(string(out), "\n")
	lines := strings.Split(str, "\n")
	if len(lines) != 1 {
		return "", TOO_MANY_VMS
	}
	parts := strings.Split(lines[0], "{")
	if len(parts) != 2 {
		return "", CANT_UNDERSTAND_OUTPUT
	}
	id := strings.Trim(parts[1], "}")
	if v.debug {
		fmt.Printf("[debug] virtual machine id is %s\n", id)
	}
	cmd = exec.Command(v.vboxmanage, "showvminfo", id, "--machinereadable")
	out, err = cmd.Output()
	if err != nil {
		return "", err
	}
	str = strings.Trim(string(out), "\n")
	lines = strings.Split(str, "\n")
	sharedName := make(map[int64]string)
	sharedPath := make(map[string]int64)
	var num string
	for _, line := range lines {
		if !strings.HasPrefix(line, "SharedFolder") {
			continue
		}
		if !strings.HasPrefix(line, "SharedFolderNameMachineMapping") &&
			!strings.HasPrefix(line, "SharedFolderPathMachineMapping") {
			panic("format of output from vboxmanage has changed")
		}
		//pick out the num part
		if strings.HasPrefix(line, "SharedFolderNameMachineMapping") {
			parts = strings.Split(line, "=")
			num = parts[0][len("SharedFolderNameMachineMapping"):]
		} else {
			parts = strings.Split(line, "=")
			num = parts[0][len("SharedFolderPathMachineMapping"):]
		}
		n, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			panic("format of output from vboxmanage has changed")
		}
		//build tables
		if strings.HasPrefix(line, "SharedFolderNameMachineMapping") {
			sharedName[n] = strings.Trim(parts[1], "\"")
		} else {
			sharedPath[strings.Trim(parts[1], "\"")] = n
		}
	}
	mapping := make(map[string]string)
	for k, v := range sharedPath {
		mapping[k] = sharedName[v]
	}
	if v.debug {
		fmt.Printf("[debug] virtualbox path mappings %+v\n", mapping)
	}
	for source, dest := range mapping {
		if strings.HasPrefix(vol, source) {
			result := "/" + dest + vol[len(source):]
			if v.debug {
				fmt.Printf("[debug] code volume %s converted to %s\n", vol, result)
			}
			return result, nil
		}
	}
	return "", errors.New(fmt.Sprintf("could not find a source mapping for %s", vol))
}
