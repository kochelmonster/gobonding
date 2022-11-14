sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1

ip tuntap add mode tun dev tun0
ip addr add 11.11.11.1/30 dev tun0
ip link set dev tun0 up

ip route add 192.168.226.0/24 via 11.11.11.2 dev tun0
ip route add 11.11.11.1/30 via 11.11.11.2 dev tun0

iptables -t nat -A POSTROUTING --j MASQUERADE

sysctl -w kern.ipc.maxsockbuf=3014656 || true
