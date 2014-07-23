package io

type SharedFolder struct {
	HostPath string
	MapsTo   string
}

type VirtualBox interface {
	FSMappings() ([]SharedFolder, error)
}

type vboxManage struct {
	debug bool
}

func NewVirtualBox(debug bool) (VirtualBox, error) {
	return &vboxManage{
		debug: debug,
	}, nil
}

func (v *vboxManage) FSMappings() ([]SharedFolder, error) {
	return nil, nil
}
