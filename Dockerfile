FROM alpine:latest

# "amd64" | "arm" | ...
ARG TARGETARCH

# using host's filesystem boosts performance. it is expected that the user uses this as anonymous
# volume (i.e. nothing specific has to be done) for this to get cleaned up
VOLUME /var/cache/edgerouter

ENTRYPOINT ["edgerouter"]

CMD ["serve"]

COPY bin/deploy-turbocharger-site.sh /usr/bin/

COPY "rel/edgerouter_linux-$TARGETARCH" /usr/bin/edgerouter
