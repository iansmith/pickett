### pickett: make for the docker world 

### How to get it

Assuming you have a modern version of go already installed:

```
mkdir -p /tmp/foo/src
export GOPATH=/tmp/foo
export PATH=$PATH:/tmp/foo/bin
go get github.com/tools/godep
godep get github.com/igneoussystems/pickett/pickett
```
You should end up with the executable `pickett` in `/tmp/foo/bin`.

### How to get a sample project

Assuming you did the above:

```
cd /tmp/foo
git clone git@github.com:igneoussystems/pickett-samples
cd pickett-samples/sample1
```

The file to examine is the `Pickett.json`.

### How to build some stuff

Assuming you 

* have a modern version of docker (at least 1.1.1) already installed 
* have set your DOCKER_HOST if you not on the machine that runs docker or are using boot2docker
* Are willing to wait, this takes several minutes the first time
```
cd /tmp/foo/pickett-samples/sample1/
pickett -debug
```

That will take a while the first time because it creates some docker images that require downloading a lot of software (`apt-get insntall blah blah`).  Once in steady state, pickett does incremental builds.  



