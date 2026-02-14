FROM alpine:3.22

RUN apk add --no-cache bash curl docker-cli coreutils

COPY watchdog.sh /watchdog.sh
RUN chmod 755 /watchdog.sh

ENTRYPOINT ["/watchdog.sh"]
