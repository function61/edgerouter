FROM alpine:3.11

ADD rel/edgerouter_linux-amd64 /usr/local/bin/edgerouter

RUN chmod +x /usr/local/bin/edgerouter
