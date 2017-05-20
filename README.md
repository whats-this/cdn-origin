# Whats-This CDN Origin

Simple but quick Golang webserver that serves requests to get files and short
URLs from a [PostgreSQL](https://www.postgresql.org) and
[SeaweedFS](https://github.com/chrislusf/seaweedfs) backend.

### Requirements

- PostgreSQL server with `objects` table
- SeaweedFS cluster with files in the `objects` table

### Usage

```
$ go get -u github.com/whats-this/cdn-origin
$ cdn-origin --help

$ # e.g.
$ cdn-origin
    --compress=true \
    --listen-addr="127.0.0.1:8000" \
    --postgres-uri="postgres://postgres:password@localhost/whats_this?sslmode=disable" \
    --seaweed-master-uri="http://localhost:9333"
```

All command-line parameters can be supplied as environment variables,
environment variables take precedence. See [cdn-origin.go#init](cdn-origin.go)
for more information.

### TODO

- [ ] Process chunked files stored in SeaweedFS, similar to [how SeaweedFS cli
  handles it](https://github.com/chrislusf/seaweedfs/wiki/Large-File-Handling)
- [ ] Add TTL to volume cache
- [ ] Write tests

### License

`cdn-origin` is licensed under the MIT license. A copy of the MIT license can be
found in [LICENSE](LICENSE).
