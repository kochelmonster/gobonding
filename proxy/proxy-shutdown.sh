set -e
iptables -t nat -D POSTROUTING --j MASQUERADE
ip route restore < /tmp/org-routing
ip link set dev tun0 down
ip tuntap del mode tun dev tun0