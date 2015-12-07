seesaw_vip <
  fqdn: "seesaw-vip1.example.com."
  ipv4: "192.168.36.16/26"
  status: PRODUCTION
>
vserver <
  name: "dns.resolver@au-syd"
  rp: "foo"
  entry_address <
    fqdn: "dns-vip1.example.com."
    ipv4: "192.168.36.1/26"
    ipv6: "2015:cafe:36::a800:1ff:ffee:dd01/64"
    status: PRODUCTION
  >
  vserver_entry <
    protocol: TCP
    port: 53
  >
  vserver_entry <
    protocol: UDP
    port: 53
  >
>
vserver <
  name: "dns.resolver.anycast@au-syd"
  rp: "foo"
  entry_address <
    fqdn: "dns-anycast.example.com."
    ipv4: "192.168.255.1/24"
    status: PRODUCTION
  >
  vserver_entry <
    protocol: UDP
    port: 53
    persistence: 100
    one_packet: true
    server_high_watermark: 0.8
    server_low_watermark: 0.4
    healthcheck <
      type: DNS
      interval: 2
      timeout: 1
      method: "A"
      send: "www.example.com"
      receive: "192.168.0.1"
      mode: DSR
    >
  >
  vserver_entry <
    persistence: 100
    protocol: TCP
    port: 53
  >
  backend: <
    host: <
      fqdn: "dns1-1.example.com."
      ipv4: "192.168.36.2/26"
      status: PRODUCTION
    >
    weight: 5
  >
  backend: <
    host: <
      fqdn: "dns1-2.example.com."
      ipv4: "192.168.36.3/26"
      status: PRODUCTION
    >
    weight: 4
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
>
vserver: <
 name: "irc.server@au-syd"
 entry_address: <
   fqdn: "irc-anycast.example.com."
   ipv4: "192.168.255.2/24"
   status: PRODUCTION
 >
 rp: "irc-team@example.com"
 vserver_entry: <
   protocol: TCP
   port: 6667
   scheduler: WLC
 >
 vserver_entry: <
   protocol: TCP
   port: 6697
   scheduler: WLC
   healthcheck: <
     type: TCP_TLS
     interval: 5
     port: 6697
     send: "ADMIN\r\n"
     receive: ":"
     tls_verify: true
     retries: 2
   >
 >
 vserver_entry: <
   protocol: TCP
   port: 80
   scheduler: WLC
 >
 backend: <
   host: <
     fqdn: "irc1-1.example.com."
     ipv4: "192.168.36.4/26"
     ipv6: "2015:cafe:36::a800:1ff:ffee:dd04/64"
     status: PRODUCTION
   >
 >
 healthcheck: <
   type: TCP
   interval: 5
   port: 6667
   send: "ADMIN\r\n"
   receive: ":"
   tls_verify: false
   retries: 2
 >
 access_grant: <
   grantee: "irc-admin"
   role: ADMIN
   type: GROUP
 >
 access_grant: <
   grantee: "irc-oncall"
   role: OPS
   type: GROUP
 >
>
dedicated_vip_subnet: "192.168.36.0/26"
dedicated_vip_subnet: "2015:cafe:36::/64"
