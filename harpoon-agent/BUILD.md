harpoon-agent
-------------

> **WIP**

### configure host

```
# fetch kernel packages
wget http://ams-mid001.int.s-cloud.net:6000/harpoon/kernel/linux-{doc,manual}-3.12.9+soundcloud_0.1_all.deb
wget http://ams-mid001.int.s-cloud.net:6000/harpoon/kernel/linux-{image,headers}-3.12.9+soundcloud_0.1_amd64.deb

# install headers, docs, and manual
sudo dpkg -i linux-headers-3.12.9+soundcloud_0.1_amd64.deb \
  linux-doc-3.12.9+soundcloud_0.1_all.deb \
  linux-manual-3.12.9+soundcloud_0.1_all.deb

# add "testing" repo for updating libc6-dev
cat <<EOF | sudo tee /etc/apt/sources.list.d/testing.list >/dev/null
deb     http://debian-mirror.int.s-cloud.net/debian testing main
deb-src http://debian-mirror.int.s-cloud.net/debian testing main
EOF
sudo apt-get update
sudo DEBIAN_FRONTEND=noninteractive \
  apt-get install -y -t testing libc6-dev

# now install image
sudo dpkg -i linux-image-3.12.9+soundcloud_0.1_amd64.deb

# reboot to load new kernel
sudo reboot
```

```
# after reboot, from local machine

scp doc/container-building/mount-cgroups.sh "${harpoon_host}":
ssh "${harpoon_host}" -- sudo sh ./mount-cgroups.sh
```

---

### build/deploy

```
# (requires linux, and updated libc6)
make build

export harpoon_host=ip-10-70-27-77.eu-west.s-cloud.net
scp bin/* "${harpoon_host}":
ssh "${harpoon_host}" -- sudo install -D harpoon-agent /srv/harpoon/bin/harpoon-agent
ssh "${harpoon_host}" -- sudo install -D harpoon-container /srv/harpoon/bin/harpoon-container

# on the harpoon host
sudo env PATH=$PATH:/srv/harpoon/bin harpoon-agent -addr ":3333"
```

---

### use

```
# from anywhere
export harpoon_host=ip-10-70-27-77.eu-west.s-cloud.net
cat <<EOF | curl -XPUT --data-binary @- http://${harpoon_host}:3333/containers/rocket-1
{
  "artifact_url": "http://files.int.s-cloud.net/bazooka/rocket-c34a033.tar.gz",
  "ports": {
    "http": 0
  },
  "env": { "FOO": "1" },
  "command": {
    "working_dir": "/srv/bazapp",
    "exec": ["./web", "-port", "\${PORT_HTTP}"]
  },
  "resources": {
    "mem": 64
  }
}
EOF

curl          http://${harpoon_host}:30000/
# stop
curl -XPOST   http://${harpoon_host}:3333/containers/rocket-1/stop
# get status
curl          http://${harpoon_host}:3333/containers/rocket-1
# start
curl -XPOST   http://${harpoon_host}:3333/containers/rocket-1/start
curl -XPOST   http://${harpoon_host}:3333/containers/rocket-1/stop
# destroy
curl -XDELETE http://${harpoon_host}:3333/containers/rocket-1
```
