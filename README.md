# Whats-This CDN Origin

Simple but quick Golang webserver that serves requests to get files and
redirects from a [PostgreSQL](https://www.postgresql.org) and
[SeaweedFS](https://github.com/chrislusf/seaweedfs) backend.

### Requirements

- PostgreSQL server with `objects` table
- SeaweedFS cluster with files in the `objects` table

### Usage

```
$ go get -u github.com/whats-this/cdn-origin

# With configuration file
$ cp $GOPATH/src/github.com/whats-this/cdn-origin/cdn-origin.sample.toml /etc/cdn-origin/cdn-origin.toml
$ vim /etc/cdn-origin/cdn-origin.toml
$ cdn-origin

# With environment variables
$ set DATABASE_CONNECTION_URL="postgres://postgres@localhost/data?sslmode=disable"
$ set SEAWEED_MASTER_URL="http://localhost:9333"
$ ...
$ cdn-origin

# With flags
$ cdn-origin \
    --database-connection-url="postgres://postgres@localhost/data?sslmode=disable" \
    --seaweed-master-url="http://localhost:9333" \
    ...

# Flags take precedence over environment variables, which take precedence over config files
```

Information about configuration variables and their purpose can be found in
[cdn-origin.sample.toml](cdn-origin.sample.toml). Configuration is handled by
[Viper](https://github.com/spf13/viper).

### Metrics

If `metrics.enable` is `true`, request metadata will be indexed in the provided
Elaticsearch server in the following format:

```js
{
  "country_code": keyword,
  "hostname":     keyword,
  "object_type":  keyword,
  "status_code":  short,
  "@timestamp":   date // generated from `@timestamp` pipeline
}
```

The index and `@timestamp` pipeline are created automatically if `cdn-origin`
has permission. Alternatively, the mapping and pipeline can be created by other
means using the `.json` files in [metrics/](metrics).

### TODO

- [ ] Process chunked files stored in SeaweedFS, similar to [how SeaweedFS cli
  handles it](https://github.com/chrislusf/seaweedfs/wiki/Large-File-Handling)
- [ ] Add TTL to volume cache
- [ ] Write tests
- [ ] Add thumbnail functionality (SeaweedFS supports this)

### License

`cdn-origin` is licensed under the MIT license. A copy of the MIT license can be
found in [LICENSE](LICENSE).
