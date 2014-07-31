#!/bin/bash

DOCKER_SHA=dacf9098702d0ee3dd969690f5f413666f341ffb

# install latest docker binaries
apt-key adv --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys 36A1D7869245C8950F966E92D8576A8BA88D21E9
echo deb https://get.docker.io/ubuntu docker main > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install lxc-docker mercurial virtualbox git apt-transport-https build-essential -y

# install docker from source
cd /tmp
git clone https://github.com/docker/docker
cd docker
git checkout DOCKER_SHA

service docker stop
docker -d &
PID=$!

make build
make binary

kill $PID
sleep 1
kill -9 $PID

sudo cp bundles/1.1.2-dev/binary/docker /usr/bin/docker

# build and install go
cd /usr/local
hg clone -u go1.3 https://code.google.com/p/go
cd go/src
./all.bash

for i in go gofmt; do
    ln -s /usr/local/go/bin/$i /usr/local/bin/
done

# build and configure etcd
cat > /etc/init/etcd.conf <<'EOF'
description "simple etcd upstart script (byron@igneous.io)"

start on started
stop on shutdown

exec /opt/etcd/bin/etcd -data-dir=/opt/etcd/datadir -name=ubuntu1404 > /var/log/etcd 2>&1
EOF
mkdir -p /opt
rm -rf /opt/etcd
git clone https://github.com/coreos/etcd /opt/etcd
cd /opt/etcd
./build
mkdir -p /opt/etcd/datadir
