ip route restore < /tmp/org-routing
ip link set dev tun0 down
ip tuntap del mode tun dev tun0