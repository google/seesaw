seesaw_vip <
  fqdn: "seesaw-vip1.example.com."
  ipv4: "192.168.36.16/26"
  status: PRODUCTION
>
vserver <
  name: "api.gateway1@as-hkg"
  rp: "foo"
  entry_address <
    fqdn: "gateway1-vip1.example.com."
    ipv4: "192.168.36.1/26"
    ipv6: "2015:cafe:36::a800:1ff:ffee:dd01/64"
    status: PRODUCTION
  >
  vserver_entry <
    protocol: TCP
    port: 443
    scheduler: MH
  >
>
