# Seesaw Getting Started Guide

## About

Seesaw is a load balancer implementation based on the Linux Virtual Server
project.  As such, it's different from the load balancers many individuals
are used to.  Unlike solutions like HAProxy or Nginx which can operate all the
way up to layer seven, Seesaw operates as a layer four load balancer.  This
means that while it can handle UDP, TCP, SCTP, AH, & ESP traffic, it does not
go far enough up the OSI stack to handle features like HTTP header
introspection, TLS termination etc.

## Requirements

In order to operate, Seesaw expects a few kernel modules to be loaded:

  * `ip_vs` - IP Virtual Server module. Used to handle transport layer
switching within the Linux kernel.
  * `nf_conntrack_ipv4` - Used by the Linux kernel to maintain the state of all
logical network connections and relate packets to the sessions.
  * `dummy` - A virtual network device which can have routes assigned but will
transfer no packets.

Each of these kernel modules should be loaded before the Seesaw process is
started.  Most users will find this easiest to achieve through the use of the
`/etc/modules.load.d` mechanism.  To load each of these automatically with the
built in `systemd` mechanisms run the following commands:

```
$ echo ip_vs > /etc/modules.load.d/ip_vs.conf
$ echo nf_conntrack_ipv4 > /etc/modules.load.d/nf_conntrack_ipv4.conf
$ echo dummy numdummies=1 > /etc/modules.load.d/dummy.conf
```

This will plumb the correct [configuration files](https://www.freedesktop.org/software/systemd/man/modules-load.d.html)
to load the kernel modules on boot.  To immediately make these changes run the
following command:

```
$ systemctl restart systemd-modules-load.service
```

Fedora/RHEL/CentOS users should note that this service is only endowed by default with the
kernel capability `CAP_SYS_MODULE` which is insufficient for loading the `ip_vs`
kernel module.

From there users should have the sufficient permissions in place to start the
service.  For testing purposes this can be started as follows:

```
$ seesaw_watchdog -logtostderr -v=10
```
