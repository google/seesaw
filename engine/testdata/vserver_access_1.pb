seesaw_vip <
  fqdn: "seesaw-vip1.example.com."
  ipv4: "192.168.255.1/26"
  status: PRODUCTION
>
vserver <
  name: "dns.resolver.anycast@au-syd"
  rp: "foo"
  entry_address <
    fqdn: "dns-anycast.example.com.",
    ipv4: "192.168.255.1/24"
    status: PRODUCTION
  >
  access_grant: <
    grantee: "user1"
    role: ADMIN
    type: USER
  >
  access_grant: <
    grantee: "sre-group1"
    role: ADMIN
    type: GROUP
  >
>
access_groups: <
  name: "sre-group1"
  member: "user2"
  member: "user3"
  member: "user4"
>