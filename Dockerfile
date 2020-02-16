FROM alpine:3.11

# for promswarmconnect
ENV METRICS_ENDPOINT=:9090/metrics

ENTRYPOINT ["edgerouter"]

CMD ["serve"]

ADD rel/edgerouter_linux-amd64 /usr/local/bin/edgerouter

RUN chmod +x /usr/local/bin/edgerouter
