seesaw_vip <
  fqdn: "seesaw-vip1.example.com."
  ipv4: "192.168.36.16/26"
  status: PRODUCTION
>
vserver <
  name: "dns.resolver@au-syd"
  rp: "foo"
  entry_address <
    fqdn: "dns-anycast.example.com."
    ipv4: "192.168.36.1/24"
    status: PRODUCTION
  >
  vserver_entry <
    protocol: UDP
    port: 53
    persistence: 100
    healthcheck <
      type: HTTP
      send: "foo"
      receive: "bar"
      code: 200
      mode: DSR
    >
  >
  vserver_entry <
    protocol: TCP
    port: 53
    persistence: 100
    healthcheck <
      type: HTTP
      send: "foo"
      receive: "bar"
      code: 200
      mode: DSR
    >
  >
  backend: <
    host: <
      fqdn: "dns1-1.example.com."
      ipv4: "192.168.37.2/26"
      status: DISABLED
    >
  >
  backend: <
    host: <
      fqdn: "dns1-2.example.com."
      ipv4: "192.168.37.3/26"
      status: DISABLED
    >
  >
>
