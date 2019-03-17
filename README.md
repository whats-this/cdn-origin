# cdn-origin

Simple but quick Golang webserver that serves requests to get files and
redirects from [PostgreSQL](https://www.postgresql.org).

### Features
- Serves files, short URLs and "tombstones" (deleted file markers)
- Allows for URL previewing on short URLs (add `?preview`)
- Allows for thumbnail generation on images (if enabled, add `?thumbnail`)
- Can be configured to store generalized metrics

### Requirements

- PostgreSQL server with `objects` table
- Access to the folder where the files are stored

### Usage

```
$ git clone https://owo.codes/whats-this/cdn-origin.git
$ cd cdn-origin
$ cp config.sample.toml config.toml
$ vim config.toml
$ go build main.go
$ ./main --config-file ./config.toml
```

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
means using the `.json` files in [lib/metrics/](lib/metrics).

### TODO

- `OPTIONS`/`HEAD` support
- Write tests

### License

`cdn-origin` is licensed under the MIT license. A copy of the MIT license can be
found in [LICENSE](LICENSE).
