# nat1

Expose your local service to Internet in NAT1 network.

* Maintain the mapped address automatically.
* Bind the mapped address to SVCB record.
* Support both TCP and UDP.

## Single mapping

Install the command line tool:

```shell
go install github.com/koho/nat1/cmd/nat1@latest
```

### Zero config

A NAT entry is automatically created on gateway.

```shell
# export local port 3389
nat1 tcp 3389
```

### Manual config

Create a public address mapping only. You need to manually configure the local address mapping on gateway.

```shell
# random local port
nat1 tcp
# fixed local port
nat1 tcp -l :5000
```

## Multiple mappings

Bind the mapped address to DNS SVCB record. Currently only `dnspod` is supported.

Install the command line tool:

```shell
go install github.com/koho/nat1/cmd/nat1s@latest
```

An example config file:

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

Run the service with the following command:

```shell
nat1s config.json
```

### Port forwarding

For each service, add corresponding forwarding rules to the router.

```shell
sudo iptables -t nat -A PREROUTING -i eth0 -p tcp --dport 50001 -j DNAT --to-destination 192.168.1.55:443
```

### Lookup domain

Your can find your mapped address using `dig` or https://www.nslookup.io/svcb-lookup/.
