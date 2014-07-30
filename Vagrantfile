#
# Vagrant file for playing with pickett. Based on Ubuntu 14.04.
# Contains etcd, docker, git and go.
#

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  # Pickett box loaded from AWS S3 bucket
  config.vm.box = "docker-runner"
  config.vm.box_url = "http://opscode-vm-bento.s3.amazonaws.com/vagrant/virtualbox/opscode_ubuntu-14.04_chef-provisionerless.box"
  config.vm.provision "shell", path: "provision.sh"

  # Forward ports for docker (:2375) and etcd (:4001)
  config.vm.network "forwarded_port", guest: 2375, host: 2375
  config.vm.network "forwarded_port", guest: 4001, host: 4001

  # Mount /vagrant via NFS
  config.vm.network "private_network", type: "dhcp"
  # Mount the host machine's home directory to /vagrant
  config.vm.synced_folder "~", "/vagrant", type: "nfs"
end
