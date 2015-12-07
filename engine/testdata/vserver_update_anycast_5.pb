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
    ipv4: "192.168.255.99/24"
    status: PRODUCTION
  >
  vserver_entry <
    protocol: UDP
    port: 53
    persistence: 1000
    healthcheck <
      type: DNS
      interval: 2
      timeout: 1
      method: "A"
      send: "dns-anycast.example.com"
      receive: "192.168.255.1"
      mode: DSR
    >
  >
  vserver_entry <
    protocol: TCP
    port: 53
    persistence: 1000
  >
  healthcheck <
    type: HTTPS
    port: 16767
    send: "/healthz"
    receive: "Ok"
    code: 200
    tls_verify: false
    mode: DSR
  >
  backend: <
    host: <
      fqdn: "dns1-2.example.com."
      ipv4: "192.168.37.2/26"
      status: PRODUCTION
    >
  >
>
