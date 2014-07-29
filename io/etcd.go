package io

import (
	"fmt"

	"github.com/coreos/go-etcd/etcd"
)

type EtcdClient interface {
	Get(string) (string, bool, error)
	Put(string, string) (string, error)
	Del(string) (string, error)
}

const (
	A_LONG_TIME = 90 * 24 * 60 * 60
)

type etcdClient struct {
	client *etcd.Client
	debug  bool
}

func NewEtcdClient(debug bool) (EtcdClient, error) {
	result := &etcdClient{
		client: etcd.NewClient([]string{constructEctdHost()}),
		debug:  debug,
	}
	/*	client, err := etcd.NewTLSClient(
				[]string{"https://iansmith.iggy.bz:4001"},
				"/home/iansmith/.docker/cert.pem",
				"/home/iansmith/.docker/key.pem",
				"/home/iansmith/.docker/ca.pem")
			if err != nil {
				panic(err)
			}
		result := &etcdClient{
			client: client,
			debug:  debug,
		}*/

	_, err := result.client.Get("/blah/blah/blah", false, false)
	if err == nil {
		panic("should not be able to retreive /blah/blah/blah")
	}
	/*fmt.Printf("ETCD SENT A RESULT! %v\n", err)*/
	e := err.(*etcd.EtcdError)
	if e.ErrorCode != 100 {
		return nil, e
	}
	return result, nil
}

func (e *etcdClient) Put(path string, value string) (string, error) {
	if e.debug {
		fmt.Printf("[etcd] PUT %s %s\n", path, value)
	}
	resp, err := e.client.Set(path, value, A_LONG_TIME)
	if err != nil {
		if e.debug {
			fmt.Printf("[etcd err] %v\n", err)
		}
		return "", err
	}
	if resp.PrevNode == nil {
		if e.debug {
			fmt.Printf("[etcd result] [none]\n")
		}
		return "", nil
	}
	if e.debug {
		fmt.Printf("[etcd result] %s\n", resp.PrevNode.Value)
	}
	return resp.PrevNode.Value, nil
}

func (e *etcdClient) Get(path string) (string, bool, error) {
	if e.debug {
		fmt.Printf("[etcd] GET %s\n", path)
	}
	resp, err := e.client.Get(path, false, false)
	if err != nil {
		detail := err.(*etcd.EtcdError)
		if detail.ErrorCode == 100 {
			if e.debug {
				fmt.Printf("[etcd result] not found\n")
			}
			return "", false, nil
		}
		if e.debug {
			fmt.Printf("[etcd err] %v\n", err)
		}
		return "", false, err
	}
	if e.debug {
		fmt.Printf("[etcd result] %s\n", resp.Node.Value)
	}
	return resp.Node.Value, true, nil
}

func (e *etcdClient) Del(path string) (string, error) {
	if e.debug {
		fmt.Printf("[etcd] DEL %s\n", path)
	}
	resp, err := e.client.Delete(path, false)
	if err != nil {
		if e.debug {
			fmt.Printf("[etcd err] %v\n", err)
		}
		return "", err
	}
	if resp.PrevNode == nil {
		if e.debug {
			fmt.Printf("[etcd result] [none]\n")
		}
		return "", nil
	}
	if e.debug {
		fmt.Printf("[etcd result] %s\n", resp.PrevNode.Value)
	}
	return resp.PrevNode.Value, nil
}
