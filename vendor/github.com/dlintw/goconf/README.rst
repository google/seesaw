Introduction
============

This package is adopt from  http://code.google.com/p/goconf/
And porting it to Go 1 spec.

INSTALL
=======
assume your local package path is $HOME/go::

  export GOPATH=$HOME/go

  # method 1:
  go get github.com/dlintw/goconf
  go test github.com/dlintw/goconf # test it

  # method 2:
  cd $GOPATH/src
  hg clone https://dlintw@github.com/dlintw/goconf.git
  cd goconf
  make
  make test # test it

USAGE
=======

sample usage::

  import "github.com/dlintw/goconf"

NOTE: All section names and options are case insensitive. All values are case sensitive.

Example 1
---------

Config::

  host = something.com
  port = 443
  active = true
  compression = off

Code::

  c, err := goconf.ReadConfigFile("something.config")
  c.GetString("default", "host") // return something.com
  c.GetInt("default", "port") // return 443
  c.GetBool("default", "active") // return true
  c.GetBool("default", "compression") // return false

Example 2
---------

Config::

  [default]
  host = something.com
  port = 443
  active = true
  compression = off

  [service-1]
  compression = on

  [service-2]
  port = 444

Code::

  c, err := goconf.ReadConfigFile("something.config")
  c.GetBool("default", "compression") // returns false
  c.GetBool("service-1", "compression") // returns true
  c.GetBool("service-2", "compression") // returns GetError

.. vi:set et sw=2 ts=2:
