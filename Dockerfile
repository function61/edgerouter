FROM alpine:latest

# using host's filesystem boosts performance. it is expected that the user uses this as anonymous
# volume (i.e. nothing specific has to be done) for this to get cleaned up
VOLUME /var/cache/edgerouter

ENTRYPOINT ["edgerouter"]

CMD ["serve"]

ADD rel/edgerouter_linux-amd64 /usr/local/bin/edgerouter
