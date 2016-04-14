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
started.  There are a number of ways that this can be completed.

### Manually

The various kerel modules can be loaded manually with the command `modprobe`:

```
$ modprobe ip_vs
$ modprobe nf_conntrack_ipv4
$ modprobe dummy numdummies=1
```
It is important to note that this process is not persistant.  These commands
will need to occur each time the kernel is reloaded.  Optionally, one of the
other mechanisms may be used.  Each method has its own advantages and
disadvantages.  Users unsure of which mechanism is most appropriate for their
situation should consult their distro documentation.

### Modprobe config files

The final mechanism for configuring the kernel modules is through the use of a 
series of modprobe configuration files. Each file is located at the path
`/etc/modprobe.d` and should be formed of the name `modulename.conf`:

```
$ echo options ip_vs > /etc/modprobe.d/ip_vs.conf
$ echo options nf_conntrack_ipv4 > /etc/modprobe.d/nf_conntrack_ipv4.conf
$ echo options dummy numdummies=1 > /etc/modprobe.d/dummy.conf
```
It should be noted that this process will only load the modules *on boot*, thus
users utilizing this mechanism will need to either reboot or run the commands
from the previous example before continuing.

### Systemd

Systemd users will find this easiest to achieve through the use of the
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

## Starting

After the modules have been loaded users should have the sufficient permissions
in place to start the service.  For testing purposes this can be started as
follows:

```
$ seesaw_watchdog -logtostderr
```
