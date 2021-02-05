FROM alpine:3.11

# for promswarmconnect
ENV METRICS_ENDPOINT=:9090/metrics

# using host's filesystem boosts performance. it is expected that the user uses this as anonymous
# volume (i.e. nothing specific has to be done) for this to get cleaned up
VOLUME /var/cache/edgerouter

ENTRYPOINT ["edgerouter"]

CMD ["serve"]

ADD rel/edgerouter_linux-amd64 /usr/local/bin/edgerouter

RUN chmod +x /usr/local/bin/edgerouter
