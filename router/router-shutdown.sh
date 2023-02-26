set -e
ip route flush table all
ip route restore < /tmp/org-routing || true
ip link set dev tun0 down
ip tuntap del mode tun dev tun0