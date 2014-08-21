package io

import (
	"github.com/coreos/go-etcd/etcd"
	"path/filepath"
	"strings"
)

type EtcdClient interface {
	Get(string) (string, bool, error)
	Put(string, string) (string, error)
	Del(string) (string, error)
	Children(string) ([]string, bool, error)
}

const (
	A_LONG_TIME = 90 * 24 * 60 * 60
)

type etcdClient struct {
	client *etcd.Client
}

func NewEtcdClient() (EtcdClient, error) {
	result := &etcdClient{
		client: etcd.NewClient([]string{constructEctdHost()}),
	}
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
func (e *etcdClient) Children(path string) ([]string, bool, error) {
	path = filepath.Clean(path)
	flog.Debugf("[etcd] CHILDREN %s", path)
	resp, err := e.client.Get(path, false, false)
	if err != nil {
		detail := err.(*etcd.EtcdError)
		if detail.ErrorCode == 100 {
			flog.Debugf("[etcd result] key was not found, so no children")
			return nil, false, nil
		}
		flog.Debugf("[etcd err] %T %+v\n", err, err)
		return nil, true, err
	}
	pathLen := len(path)
	if !strings.HasSuffix(path, "/") {
		pathLen += 1
	}
	result := []string{}
	for _, r := range resp.Node.Nodes {
		result = append(result, r.Key[pathLen:])
	}
	flog.Debugf("[etcd response] %s", result)
	return result, true, nil
}

func (e *etcdClient) Put(path string, value string) (string, error) {
	flog.Debugf("[etcd] PUT %s %s", path, value)
	resp, err := e.client.Set(path, value, A_LONG_TIME)
	if err != nil {
		flog.Debugf("[etcd err] %v", err)
		return "", err
	}
	if resp.PrevNode == nil {
		flog.Debugf("[etcd result] [none]")
		return "", nil
	}
	flog.Debugf("[etcd result] %s", resp.PrevNode.Value)
	return resp.PrevNode.Value, nil
}

func (e *etcdClient) Get(path string) (string, bool, error) {
	flog.Debugf("[etcd] GET %s", path)
	resp, err := e.client.Get(path, false, false)
	if err != nil {
		detail := err.(*etcd.EtcdError)
		if detail.ErrorCode == 100 {
			flog.Debugf("[etcd result] not found")
			return "", false, nil
		}
		flog.Debugf("[etcd err] %v", err)
		return "", false, err
	}
	flog.Debugf("[etcd result] %s", resp.Node.Value)
	return resp.Node.Value, true, nil
}

func (e *etcdClient) Del(path string) (string, error) {
	flog.Debugf("[etcd] DEL %s", path)
	resp, err := e.client.Delete(path, false)
	if err != nil {
		flog.Debugf("[etcd err] %v", err)
		return "", err
	}
	if resp.PrevNode == nil {
		flog.Debugf("[etcd result] [none]")
		return "", nil
	}
	flog.Debugf("[etcd result] %s", resp.PrevNode.Value)
	return resp.PrevNode.Value, nil
}
