# gobonding

A wide area network bonder written in go.

![Overview](https://github.com/kochelmonster/gobonding/blob/main/assets/overview.png?raw=true)

- The router tunnels IP-packets via UDP to the proxy.
- The protocol is kept very simple for the lowest possible overhead.
- The router authenticates to the proxy by a RSA challenge response dialog.
- gobonding can be installed on any linux distribution. The only requirement is a TUN/TAP device driver.

## Requirements

gobonding constantly measures transmission speed and therefore requires synchonized clocks.
For successfully running gobonding you have to install time synchronization services like ntpd or timesyncd.

## Usage

Für amd64 download gobonding.tar.gz to your router and extract under `/usr/local`.
Execute

```bash
/usr/local/gobonding/config [IP of Proxy] [Local IP of Modem 1] [Local IP of Modem 2]
```

Check the following files and change them if necessary:

- `/usr/local/gobonding/router/router-setup.sh`
- `/usr/local/gobonding/router/gobonding.yml`
- `/usr/local/gobonding/proxy/gobonding.yml`

```bash
cp /usr/local/gobonding/router/gobonding-router.service /etc/systemd/system
systemctl enable gobonding-router
systemctl start gobonding-router
```

Copy the directory `/usr/local/gobonding/proxy` to your VPS (proxy).
On the VPS:

```bash
cp /usr/local/gobonding/proxy/gobonding-proxy.service /etc/systemd/system
systemctl enable gobonding-proxy
systemctl start gobonding-proxy
```

For other platforms, compile this package in go. To create gobonding.tar.gz execute

```bash
sh package.sh
```

## State

Gobonding runs as expected in a vm-ware simulation with trottled networks. Unfortunately it does not work in the setup in which I wanted to use it. (see Motivation)

## Motivation

I live in a place where only mobile internet delivers decent speed at an affordable price. There are no copper lines or even fiber optic connections. My idea was to use data SIM cards from different providers and use my wife's and I's smartphone as a modem via tethering: when we're both at home, we surf with double speed, if only one is there, at least at single speed.

After short research I found the [OpenMPTCP](https://www.openmptcprouter.com/) project.
Unfortunately, I first had to find a VPS provider that could offer the prerequisite for the OpenMPTCP Proxy, many providers cannot. After I found one, the proxy was installed without any problems, just like the router. And the web GUI of the router shows me quite quickly that it is connected to the proxy via both channels.

The first speed test was sobering, instead of double the speed, it was transmitted at a fraction of the speed of a single channel.

I now had a choice: tinker with a system that I only understood rudimentarily, or write something new. Since I'm a developer and not an admin, the choice was easy for me and
gobonding was born.

The first Version of gobonding based on tcp, delivers almost exact the same results as OpenMPTCP. By changing to UDP and reducing as much overhead as I could, the transmission
speed improved dramatically but never to the desired (almost) doubling.
I setup a simulated system in vm-ware and it worked as expected.

### Why does gobonding not work in the smarphone setup?

The answer is: the transmission speed of mobile internet, at least in my place, constantly and unpredictably changes very fast.

The problem is: To increase the speed to more than one single channel, the distribution of packets must be equal to the speed of channels.

Assuming channel A is twice as fast as channel B, we only get 1.5 times the speed of A if channel A sends twice the amount of data than channel B.

If the same amount of data were sent over both channels, the proxy would have to wait for B's data and the combined speed would then not be higher than B's speed.

Because of the highly dynamic change of transmission speed of the mobile internet, the
right distribution of packages is impossible and the speed is always the minimum of
all channels.

Any Ideas that could solve this problem are welcome.
