# Grass
![](https://github.com/k4yt3x/flowerhd/raw/master/PNG/%E8%8D%89.PNG)

Grass is a Peer-to-Peer VPN.

## Usage
Clone this repo, run `go build main.go` in the bin directory.

Sample config:

```json
{
    "Id": 114514, // some random number
    "Key": "some_key111", // pre-shared key
    "IPv4": "10.56.0.2", // ipv4 for this client, ipv6 support will be added soon
    "IPv4Gateway": "10.56.0.1", // ipv4 gateway, in fact useless
    "IPv4Mask": "255.255.255.0",
    "IPv4DNS": "8.8.8.8,8.8.4.4", // seems only useful on windows
    "Listen": [
        {
            "LocalAddr": "0.0.0.0",
            "NetworkAddr": "192.168.254.1", // your ip on external network
            "Port": 14514
        },
        {
            "LocalAddr": "[::]",
            "NetworkAddr": "[fe80:114:514:1919:810::233]",
            "Port": 14515
        }
    ],
    "BootstrapNodes": ["11.4.5.14:1919"],
    "CheckClientsInterval": 1,
    "PingInterval": 20,
    "CheckConnInterval": 5,
    "Debug": false,
    "ConnectNewPeer": true, // if false, the client will not discover new clients
    "StartTun": true // if false, the client will not start tunnel interface
}
```