package io

import (
	"fmt"

	"github.com/coreos/go-etcd/etcd"
)

type EtcdClient interface {
	Get(string) (string, bool, error)
}

type etcdClient struct {
	client *etcd.Client
	debug  bool
}

func NewEtcdClient(debug bool) (EtcdClient, error) {
	result := &etcdClient{
		client: etcd.NewClient([]string{constructEctdHost()}),
		debug:  debug,
	}
	fmt.Printf("XXX--ETCD--XXXX SHOULD TEST CONNECTION\n")
	return result, nil
}

func (e *etcdClient) Get(path string) (string, bool, error) {
	resp, err := e.client.Get(path, false, false)
	if err != nil {
		return "", false, err
	}
	fmt.Printf("XXX %+v\n", resp)
	return resp.Node.Value, true, nil
}
