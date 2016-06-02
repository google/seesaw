# Seesaw v2

[![GoDoc](https://godoc.org/github.com/google/seesaw?status.svg)](https://godoc.org/github.com/google/seesaw)

Note: This is not an official Google product.

## About

Seesaw v2 is a Linux Virtual Server (LVS) based load balancing platform.

It is capable of providing basic load balancing for servers that are on the
same network, through to advanced load balancing functionality such as anycast,
Direct Server Return (DSR), support for multiple VLANs and centralised
configuration.

Above all, it is designed to be reliable and easy to maintain.

## Requirements

A Seesaw v2 load balancing cluster requires two Seesaw nodes - these can be
physical machines or virtual instances. Each node must have two network
interfaces - one for the host itself and the other for the cluster VIP. All
four interfaces should be connected to the same layer 2 network.

## Building

Seesaw v2 is developed in Go and depends on several Go packages:

- [golang.org/x/crypto/ssh](http://godoc.org/golang.org/x/crypto/ssh)
- [github.com/dlintw/goconf](http://godoc.org/github.com/dlintw/goconf)
- [github.com/golang/glog](http://godoc.org/github.com/golang/glog)
- [github.com/golang/protobuf/proto](http://godoc.org/github.com/golang/protobuf/proto)
- [github.com/miekg/dns](http://godoc.org/github.com/miekg/dns)

Additionally, there is a compile and runtime dependency on
[libnl](https://www.infradead.org/~tgr/libnl/) and a compile time dependency on
the Go protobuf compiler.

On a Debian/Ubuntu style system, you should be able to prepare for building
by running:

    apt-get install golang
    apt-get install libnl-3-dev libnl-genl-3-dev

If your distro has a go version before 1.5, you may need to fetch a newer
release from https://golang.org/dl/.

After setting `GOPATH` to an appropriate location (for example `~/go`):

    go get -u golang.org/x/crypto/ssh
    go get -u github.com/dlintw/goconf
    go get -u github.com/golang/glog
    go get -u github.com/miekg/dns
    go get -u github.com/kylelemons/godebug/pretty

Ensure that `${GOPATH}/bin` is in your `${PATH}` and in the seesaw directory:

    make test
    make install

If you wish to regenerate the protobuf code, the protobuf compiler and Go
protobuf compiler generator are also needed:

    apt-get install protobuf-compiler
    go get -u github.com/golang/protobuf/{proto,protoc-gen-go}

The protobuf code can then be regenerated with:

    make proto

## Installing

After `make install` has run successfully, there should be a number of
binaries in `${GOPATH}/bin` with a `seesaw_` prefix. Install these to the
appropriate locations:

    SEESAW_BIN="/usr/local/seesaw"
    SEESAW_ETC="/etc/seesaw"
    SEESAW_LOG="/var/log/seesaw"

    INIT=`ps -p 1 -o comm=`

    install -d "${SEESAW_BIN}" "${SEESAW_ETC}" "${SEESAW_LOG}"

    install "${GOPATH}/bin/seesaw_cli" /usr/bin/seesaw

    for component in {ecu,engine,ha,healthcheck,ncc,watchdog}; do
      install "${GOPATH}/bin/seesaw_${component}" "${SEESAW_BIN}"
    done

    if [ $INIT = "init" ]; then
      install "etc/init/seesaw_watchdog.conf" "/etc/init"
    elif [ $INIT = "systemd" ]; then
      install "etc/systemd/system/seesaw_watchdog.service" "/etc/systemd/system"
      systemctl --system daemon-reload
    fi
    install "etc/seesaw/watchdog.cfg" "${SEESAW_ETC}"

    # Enable CAP_NET_RAW for seesaw binaries that require raw sockets.
    /sbin/setcap cap_net_raw+ep "${SEESAW_BIN}/seesaw_ha"
    /sbin/setcap cap_net_raw+ep "${SEESAW_BIN}/seesaw_healthcheck"

The `setcap` binary can be found in the libcap2-bin package on Debian/Ubuntu.

## Configuring

Each node needs a `/etc/seesaw/seesaw.cfg` configuration file, which provides
information about the node and who its peer is. Additionally, each load
balancing cluster needs a cluster configuration, which is in the form of a
text-based protobuf - this is stored in `/etc/seesaw/cluster.pb`.

An example seesaw.cfg file can be found in
[etc/seesaw/seesaw.cfg.example](etc/seesaw/seesaw.cfg.example) - a minimal
seesaw.cfg provides the following:

- `anycast_enabled` - True if anycast should be enabled for this cluster.
- `name` - The short name of this cluster.
- `node_ipv4` - The IPv4 address of this Seesaw node.
- `peer_ipv4` - The IPv4 address of our peer Seesaw node.
- `vip_ipv4` - The IPv4 address for this cluster VIP.

The VIP floats between the Seesaw nodes and is only active on the current
master. This address needs to be allocated within the same netblock as both
the node IP address and peer IP address.

An example cluster.pb file can be found in
[etc/seesaw/cluster.pb.example](etc/seesaw/cluster.pb.example) - a minimal
`cluster.pb` contains a `seesaw_vip` entry and two `node` entries. For each
service that you want to load balance, a separate `vserver` entry is
needed, with one or more `vserver_entry` sections (one per port/proto pair),
one or more `backends` and one or more `healthchecks`. Further information
is available in the protobuf definition - see
[pb/config/config.proto](pb/config/config.proto).

On an upstart based system, running `restart seesaw_watchdog` will start (or
restart) the watchdog process, which will in turn start the other components.

### Anycast

Seesaw v2 provides full support for anycast VIPs - that is, it will advertise
an anycast VIP when it becomes available and will withdraw the anycast VIP if
it becomes unavailable. For this to work the Quagga BGP daemon needs to be
installed and configured, with the BGP peers accepting host-specific routes
that are advertised from the Seesaw nodes within the anycast range (currently
hardcoded as `192.168.255.0/24`).

## Command Line

Once initial configuration has been performed and the Seesaw components are
running, the state of the Seesaw can be viewed and controlled via the Seesaw
command line interface. Running `seesaw` (assuming `/usr/bin` is in your path)
will give you an interactive prompt - type `?` for a list of top level
commands. A quick summary:

- `config reload` - reload the cluster.pb from the current config source.
- `failover` - failover between the Seesaw nodes.
- `show vservers` - list all vservers configured on this cluster.
- `show vserver <name>` - show the current state for the named vserver.

## Troubleshooting

A Seesaw should have five components that are running under the watchdog - the
process table should show processes for:

- `seesaw_ecu`
- `seesaw_engine`
- `seesaw_ha`
- `seesaw_healthcheck`
- `seesaw_ncc`
- `seesaw_watchdog`

All Seesaw v2 components have their own logs, in addition to the logging
provided by the watchdog. If any of the processes are not running, check the
corresponding logs in `/var/log/seesaw` (e.g. `seesaw_engine.{log,INFO}`).
