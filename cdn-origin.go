package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"strings"
	"time"
	"unsafe"

	"github.com/whats-this/cdn-origin/prometheus"
	"github.com/whats-this/cdn-origin/weed"

	log "github.com/Sirupsen/logrus"
	_ "github.com/lib/pq"
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/valyala/fasthttp"
)

// version is the current version of cdn-origin.
const version = "0.2.0"

// HTTP header strings.
const (
	accept = "Accept"
	host   = "Host"
)

// cdnUtil is the path to CDN utilities endpoint.
const cdnUtil = "/.cdn-util"

// authKey is the query parameter for authentication on the CDN utilities endpoint.
const keyParam = "authKey"

// redirectHTML is the html/template template for generating redirect HTML.
const redirectHTML = `<html><head><meta charset="UTF-8" /><meta http-equiv=refresh content="0; url={{.}}" /><script type="text/javascript">window.location.href="{{.}}"</script><title>Redirect</title></head><body><p>If you are not redirected automatically, click <a href="{{.}}">here</a> to go to the destination.</p></body></html>`

var redirectHTMLTemplate *template.Template

// redirectPreviewHTML is the html/template template for generating redirect preview HTML.
const redirectPreviewHTML = `<html><head><meta charset="UTF-8" /><title>Redirect Preview</title></head><body><p>This link goes to <code>{{.}}</code>. If you would like to visit this link, click <a href="{{.}}">here</a> to go to the destination.</p></body></html>`

var redirectPreviewHTMLTemplate *template.Template

func init() {
	// Read in configuration
	flags := pflag.NewFlagSet("cdn-origin", pflag.ExitOnError)

	// cdnUtil.authKey (string=""): authentication token for accessing /.cdn-util (cdnUtil)
	flags.String("cdnutil-auth-key", "", "Authentication token for accessing /.cdn-util (util)")
	viper.BindPFlag("cdnUtil.authKey", flags.Lookup("cdnutil-auth-key"))
	viper.BindEnv("cdnUtil.authKey", "CDNUTIL_AUTH_KEY")

	// cdnUtil.serveMetrics (bool=false): serve Prometheus metrics (cdnUtil)
	flags.Bool("cdnutil-serve-metrics", false, "Serve Prometheus metrics (cdnUtil)")
	viper.BindPFlag("cdnUtil.serveMetrics", flags.Lookup("cdnutil-serve-metrics"))
	viper.BindEnv("cdnUtil.serveMetrics", "CDNUTIL_SERVE_METRICS")

	// http.compressResponse (bool=false): enable transparent response compression
	flags.Bool("compress-response", false, "Enable transparent response compression")
	viper.BindPFlag("http.compressResponse", flags.Lookup("compress-response"))
	viper.BindEnv("http.compressResponse", "HTTP_COMPRESS_RESPONSE")

	// http.listenAddress (string=":8080"): TCP address to listen to for HTTP requests
	flags.String("listen-address", ":8080", "TCP address to listen to for HTTP requests")
	viper.BindPFlag("http.listenAddress", flags.Lookup("listen-address"))
	viper.BindEnv("http.listenAddress", "HTTP_LISTEN_ADDRESS")

	// log.debug (bool=false): enable debug mode (logs requests and prints other information)
	flags.Bool("debug", false, "Enable debug mode (logs requests and prints other information)")
	viper.BindPFlag("log.debug", flags.Lookup("debug"))
	viper.BindEnv("log.debug", "DEBUG")

	// database.connectionURL* (string): PostgreSQL connection URL
	flags.String("database-connection-url", "", "* PostgreSQL connection URL")
	viper.BindPFlag("database.connectionURL", flags.Lookup("database-connection-url"))
	viper.BindEnv("database.connectionURL", "DATABASE_CONNECTION_URL")

	// seaweed.masterURL* (string): SeaweedFS master URL
	flags.String("seaweed-master-url", "", "* SeaweedFS master URL")
	viper.BindPFlag("seaweed.masterURL", flags.Lookup("seaweed-master-url"))
	viper.BindEnv("seaweed.masterURL", "SEAWEED_MASTER_URL")

	// Configuration file settings
	viper.SetConfigType("toml")
	viper.SetConfigName("cdn-origin")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/cdn-origin/")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			log.WithField("err", err).Fatal("failed to read in configuration")
		}
	}

	// Enable debug mode
	if viper.GetBool("log.debug") {
		log.SetLevel(log.DebugLevel)
	}

	// Print configuration variables to debug
	log.WithFields(log.Fields{
		"cdnUtil.authKey":        len(viper.GetString("cdnUtil.authKey")) != 0,
		"cdnUtil.serveMetrics":   viper.GetBool("cdnUtil.serveMetrics"),
		"database.connectionURL": viper.GetString("database.connectionURL"),
		"http.compressResponse":  viper.GetBool("http.compressResponse"),
		"http.listenAddress":     viper.GetString("http.listenAddress"),
		"log.debug":              viper.GetBool("log.debug"),
		"seaweed.masterURL":      viper.GetString("seaweed.masterURL"),
	}).Debug("retrieved configuration")

	// Ensure required configuration variables are set
	if len(viper.GetString("database.connectionURL")) == 0 {
		log.Fatal("database.connectionURL is required")
	}
	if len(viper.GetString("seaweed.masterURL")) == 0 {
		log.Fatal("seaweed.masterURL is required")
	}
}

// db holds the current PostgreSQL database connection.
var db *sql.DB

// seaweed client to use for fetching files from the SeaweedFS cluster.
var seaweed *weed.Seaweed

func main() {
	var err error

	// Attempt to connect to SeaweedFS master
	seaweed = weed.New(viper.GetString("seaweed.masterURL"), time.Second*5)
	err = seaweed.Ping()
	if err != nil {
		log.WithField("err", err).Fatal("failed to ping SeaweedFS master")
		return
	}

	// Connect to PostgreSQL database
	db, err = sql.Open("postgres", viper.GetString("database.connectionURL"))
	if err != nil {
		log.WithField("err", err).Fatal("failed to open database connection")
		return
	}

	// Parse redirect templates
	redirectHTMLTemplate, err = template.New("redirectHTML").Parse(redirectHTML)
	if err != nil {
		log.WithField("err", err).Fatal("failed to parse redirectHTML template")
		return
	}
	redirectPreviewHTMLTemplate, err = template.New("redirectPreviewHTML").Parse(redirectPreviewHTML)
	if err != nil {
		log.WithField("err", err).Fatal("failed to parse redirectPreviewHTML template")
		return
	}

	// Launch server
	h := requestHandler
	if viper.GetBool("http.compressResponse") {
		h = fasthttp.CompressHandler(h)
	}
	log.Info("Attempting to listen on " + viper.GetString("http.listenAddress"))
	server := &fasthttp.Server{
		Handler:                       h,
		Name:                          "whats-this/cdn-origin/" + version,
		ReadBufferSize:                1024 * 6, // 6 KB
		ReadTimeout:                   time.Minute * 30,
		WriteTimeout:                  time.Minute * 30,
		GetOnly:                       true,
		LogAllErrors:                  log.GetLevel() == log.DebugLevel,
		DisableHeaderNamesNormalizing: true,
		Logger: log.New(),
	}
	if err := server.ListenAndServe(viper.GetString("http.listenAddress")); err != nil {
		log.WithField("err", err).Fatal("error in ListenAndServe")
	}
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())

	// Log requests in debug mode, wrapped in an if statement to prevent unnecessary memory allocations
	if log.GetLevel() == log.DebugLevel {
		log.WithFields(log.Fields{
			"connRequestNumber": ctx.ConnRequestNum(),
			// "connTime":          ctx.ConnTime(),
			"method": string(ctx.Method()),
			// "path":        path,
			"queryString": ctx.QueryArgs(),
			"remoteIP":    ctx.RemoteIP(),
			"requestURI":  string(ctx.RequestURI()),
			// "time":              ctx.Time(),
			// "userAgent":         string(ctx.UserAgent()),
		}).Debug("request received")
	}

	// CDN utilities endpoint (only accessible if key is supplied and correct)
	if len(viper.GetString("cdnUtil.authKey")) != 0 && strings.HasPrefix(path, cdnUtil) && string(ctx.QueryArgs().Peek(keyParam)) == viper.GetString("cdnUtil.authKey") {
		switch path[len(cdnUtil):] {
		case "/prometheus/metrics":
			if !viper.GetBool("cdnUtil.serveMetrics") {
				break
			}
			contentType, err := prometheus.WriteMetrics(ctx, string(ctx.Request.Header.Peek(accept)))
			if err != nil {
				log.WithField("err", err).Error("failed to generate util Prometheus response")
				ctx.SetStatusCode(fasthttp.StatusInternalServerError)
				ctx.SetContentType("text/plain; charset=utf8")
				fmt.Fprint(ctx, "500 Internal Server Error")
				return
			}
			ctx.SetContentType(contentType)
			return
		}

		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetContentType("text/plain; charset=utf8")
		fmt.Fprintf(ctx, "404 Not Found: %s", ctx.Path())
		return
	}

	// Update metrics if metrics are being served
	if viper.GetBool("cdnUtil.serveMetrics") {
		host := ctx.Request.Header.Peek(host)
		if len(host) != 0 {
			prometheus.HTTPRequestsTotal.With(prom.Labels{"host": string(host)}).Inc()
		}
	}

	// Fetch object from database
	var backend_file_id sql.NullString
	var content_type sql.NullString
	var dest_url sql.NullString
	var object_type int
	err := db.QueryRow(
		`SELECT backend_file_id, content_type, dest_url, "type" FROM objects WHERE bucket_key=$1 LIMIT 1`,
		fmt.Sprintf("public%s", path)).Scan(&backend_file_id, &content_type, &dest_url, &object_type)
	switch {
	case err == sql.ErrNoRows:
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetContentType("text/plain; charset=utf8")
		fmt.Fprintf(ctx, "404 Not Found: %s", ctx.Path())
		return
	case err != nil:
		log.WithField("err", err).Error("failed to run SELECT query on database")
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetContentType("text/plain; charset=utf8")
		fmt.Fprint(ctx, "500 Internal Server Error")
		return
	}

	switch object_type {
	case 0: // file
		// Get object from SeaweedFS and write to response
		if !backend_file_id.Valid {
			log.WithField("bucket_key", path).Warn("found file object with NULL backend_file_id")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
		}
		if content_type.Valid {
			ctx.SetContentType(content_type.String)
		} else {
			ctx.SetContentType("application/octet-stream")
		}

		// TODO: ?thumbnail query parameter for images
		statusCode, err := seaweed.Get(ctx, backend_file_id.String, "")
		if err != nil {
			log.WithField("err", err).Warn("failed to retrieve file from SeaweedFS volume server")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
			return
		}
		if statusCode != fasthttp.StatusOK {
			log.WithFields(log.Fields{
				"expected": fasthttp.StatusOK,
				"got":      statusCode,
			}).Warn("unexpected status code while retrieving file from SeaweedFS volume server")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
		}

	case 1: // redirect
		if !dest_url.Valid {
			log.Warn("encountered redirect object with NULL dest_url")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
		}

		if ctx.QueryArgs().Has("preview") {
			buf := new(bytes.Buffer)
			err := redirectPreviewHTMLTemplate.Execute(buf, dest_url.String)
			if err != nil {
				log.WithFields(log.Fields{
					"err":      err,
					"dest_url": dest_url.String,
				}).Warn("failed to generate redirect preview HTML to send to client")
				ctx.SetContentType("text/plain; charset=utf8")
				fmt.Fprintf(ctx, "Failed to generate preview page, destination URL: %s", dest_url.String)
				return
			}
			b := buf.Bytes()
			s := *(*string)(unsafe.Pointer(&b))
			ctx.SetContentType("text/html; charset=utf8")
			fmt.Fprint(ctx, s)
		} else {
			buf := new(bytes.Buffer)
			err := redirectHTMLTemplate.Execute(buf, dest_url.String)
			if err != nil {
				log.WithFields(log.Fields{
					"err":      err,
					"dest_url": dest_url.String,
				}).Warn("failed to generate redirect HTML to send to client")
				ctx.SetContentType("text/plain; charset=utf8")
				fmt.Fprintf(ctx, "Failed to generate HTML fallback page, destination URL: %s", dest_url.String)
				return
			}
			b := buf.Bytes()
			s := *(*string)(unsafe.Pointer(&b))
			ctx.SetStatusCode(fasthttp.StatusMovedPermanently)
			ctx.SetContentType("text/html; charset=utf8")
			ctx.Response.Header.Set("Location", dest_url.String)
			fmt.Fprint(ctx, s)

		}
	}
}
