FROM golang:alpine

RUN apk add --no-cache git build-base libpng-dev

COPY go.mod /git/owo.codes/whats-this/cdn-origin/
COPY go.sum /git/owo.codes/whats-this/cdn-origin/
COPY main.go /git/owo.codes/whats-this/cdn-origin/
COPY lib /git/owo.codes/whats-this/cdn-origin/lib

RUN cd /git/owo.codes/whats-this/cdn-origin && \
    go build main.go && \
    apk del git build-base

WORKDIR /git/owo.codes/whats-this/cdn-origin
ENTRYPOINT ["./main"]

