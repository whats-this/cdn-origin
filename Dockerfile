FROM golang:alpine

COPY go.mod /git/owo.codes/whats-this/cdn-origin/
COPY go.sum /git/owo.codes/whats-this/cdn-origin/
COPY main.go /git/owo.codes/whats-this/cdn-origin/
COPY lib /git/owo.codes/whats-this/cdn-origin/lib

RUN apk add --no-cache --virtual .build-deps git build-base && \
    cd /git/owo.codes/whats-this/cdn-origin && \
    go build main.go && \
    apk del .build-deps

WORKDIR /git/owo.codes/whats-this/cdn-origin
ENTRYPOINT ["./main"]

