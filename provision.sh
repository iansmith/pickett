#!/bin/bash

DOCKER_SHA=dacf9098702d0ee3dd969690f5f413666f341ffb
# install picket pre-reqs
apt-get install mercurial virtualbox git apt-transport-https -y

# install latest docker binaries
apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 36A1D7869245C8950F966E92D8576A8BA88D21E9
mkdir -p /etc/apt/sources.list.d
sh -c "echo deb https://get.docker.io/ubuntu docker main > /etc/apt/sources.list.d/docker.list"
apt-get update
apt-get install lxc-docker -y

# install docker from source
git clone https://github.com/docker/docker
cd docker
git checkout DOCKER_SHA

service docker stop
docker -d &

make build
make binary

ps auxw | grep docker | grep -v grep | awk '{print $2}' | xargs kill -SIGHUP

sudo cp bundles/1.1.2-dev/binary/docker /usr/bin/docker
