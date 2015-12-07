seesaw_vip <
  fqdn: "seesaw-vip1.example.com."
  ipv4: "192.168.36.16/26"
  ipv6: "2015:0:cafe::10/64"
  status: PRODUCTION
>
vlan <
  vlan_id: 100
  host: <
    fqdn: "seesaw1-vlan100.example.com."
    ipv4: "192.168.100.0/24"
    ipv6: "2015:100:cafe::10/64"
  >
>
vlan <
  vlan_id: 101
  host: <
    fqdn: "seesaw1-vlan101.example.com."
    ipv4: "192.168.101.0/24"
    ipv6: "2015:101:cafe::10/64"
  >
>
vserver <
  name: "dns.resolver@au-syd"
  rp: "foo"
  entry_address <
    fqdn: "dns-vip1.example.com."
    ipv4: "192.168.100.1/24"
    ipv6: "2015:101:cafe::1001/64"
    status: PRODUCTION
  >
  backend: <
    host: <
      fqdn: "dns1-1.example.com."
      ipv4: "192.168.36.2/26"
      ipv6: "2015:0:cafe::a800:1ff:ffee:dd01/64"
      status: PRODUCTION
    >
  >
  backend: <
    host: <
      fqdn: "dns1-2.example.com."
      ipv4: "192.168.100.2/26"
      status: PRODUCTION
    >
  >
  backend: <
    host: <
      fqdn: "dns1-3.example.com."
      ipv6: "2015:101:cafe::a800:1ff:ffee:dd02/64"
      status: PRODUCTION
    >
  >
  backend: <
    host: <
      fqdn: "dns1-4.example.com."
      ipv4: "192.168.100.4/26"
      ipv6: "2015:100:cafe::a800:1ff:ffee:dd03/64"
      status: PRODUCTION
    >
  >
  backend: <
    host: <
      fqdn: "dns1-5.example.com."
      ipv4: "192.168.101.5/26"
      ipv6: "2015:101:cafe::a800:1ff:ffee:dd04/64"
      status: PRODUCTION
    >
  >
>
