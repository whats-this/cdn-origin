FROM golang:alpine

COPY cdn-origin.go $GOPATH/src/github.com/whats-this/cdn-origin/
COPY prometheus/ $GOPATH/src/github.com/whats-this/cdn-origin/prometheus/
COPY weed/ $GOPATH/src/github.com/whats-this/cdn-origin/weed/

RUN apk add --no-cache --virtual .build-deps git && \

    go-wrapper download github.com/lib/pq && \
    go-wrapper download github.com/prometheus/client_golang/prometheus && \
    go-wrapper download github.com/Sirupsen/logrus && \
    go-wrapper download github.com/spf13/pflag && \
    go-wrapper download github.com/spf13/viper && \
    go-wrapper download github.com/valyala/fasthttp && \

    go-wrapper install github.com/whats-this/cdn-origin && \
    apk del .build-deps

WORKDIR $GOPATH/src/github.com/whats-this/cdn-origin
ENTRYPOINT ["go-wrapper", "run"]
