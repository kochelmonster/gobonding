cd config
go build
cd ..
cd router
go build
cd ..
cd proxy
go build
cd ..

tar -C .. -cf gobonding.tar.gz gobonding/config/config gobonding/proxy/proxy \
    gobonding/proxy/proxy-setup.sh gobonding/proxy/proxy-shutdown.sh \
    gobonding/router/router gobonding/router/router-shutdown.sh\
    gobonding/proxy/gobonding-proxy.service gobonding/router/gobonding-router.service