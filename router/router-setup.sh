ip route save table all > /tmp/org-routing
sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1

ip tuntap add mode tun dev tun0
ip addr add 11.11.11.2/30 dev tun0
ip link set dev tun0 up

ip route flush 0/0
ip route add default via 11.11.11.1 dev tun0

sysctl -w kern.ipc.maxsockbuf=3014656 || true
