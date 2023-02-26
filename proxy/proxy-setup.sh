set -e
ip route save table all > /tmp/org-routing
sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1
sysctl -w net.core.rmem_max=2500000

ip tuntap add mode tun dev tun0 || true
ip addr add 10.0.0.1/30 dev tun0 || true
ip link set dev tun0 up || true

ip route add 192.0.0.0/8 via 10.0.0.2 dev tun0 || true
ip route add 10.0.0.0/30 via 10.0.0.2 dev tun0 || true

iptables -t nat -A POSTROUTING --j MASQUERADE

