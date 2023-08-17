# nat1s

Expose your local service to Internet in NAT1 network.

* Maintain the mapped address automatically.
* Bind the mapped address to SVCB record.
* Support both TCP and UDP.

## Usage

### Map address

Currently only dnspod is supported.

```json
{
  "dns": "xxx.dnspod.net:53",
  "dnspod": {
    "secret_id": "",
    "secret_key": ""
  },
  "service": [
    {
      "domain": "svc1.example.com",
      "local": "0.0.0.0:50000",
      "alpn": ["wg"],
      "network": "udp"  
    },
    {
      "domain": "svc2.example.com",
      "local": "0.0.0.0:50001",
      "alpn": ["h2"],
      "hint": true,
      "network": "tcp"
    }
  ]
}
```

### Port forwarding

For each service, add corresponding forwarding rules to the router.

```shell
sudo iptables -t nat -A PREROUTING -i eth0 -p tcp --dport 50001 -j DNAT --to-destination 192.168.1.55:443
```

### Lookup domain

Your can find your mapped address using `dig` or https://www.nslookup.io/svcb-lookup/.
