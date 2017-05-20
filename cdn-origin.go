package main

import (
	"database/sql"
	"flag"
	"fmt"
	"html/template"
	"os"
	"strconv"
	"time"

	"github.com/whats-this/cdn-origin/weed"

	"bytes"
	log "github.com/Sirupsen/logrus"
	_ "github.com/lib/pq"
	"github.com/valyala/fasthttp"
	"unsafe"
)

// redirectHTML is the html/template template for generating redirect HTML.
const redirectHTML = `<html><head><meta charset=UTF-8 /><meta http-equiv=refresh content="0; url={{.}}" /><script type="text/javascript">window.location.href="{{.}}"</script><title>Redirect</title></head><body><p>If you are not redirected automatically, click <a href="{{.}}">here</a> to go to the destination.</p></body></html>`

var redirectHTMLTemplate *template.Template

// redirectPreviewHTML is the html/template template for generating redirect preview HTML.
const redirectPreviewHTML = `<html><head><meta charset=UTF-8 /><title>Redirect Preview</title></head><body><p>This link goes to <code>{{.}}</code>. If you would like to visit this link, click <a href="{{.}}">here</a> to go to the destination..</p></body></html>`

var redirectPreviewHTMLTemplate *template.Template

// Configuration variables from command-line or environment.
var (
	compress         bool
	debugMode        bool
	listenAddr       string
	postgresURI      string
	seaweedMasterURI string
)

func init() {
	// Collect configuration from flags
	flag.BoolVar(&compress, "compress", false, "Enable transparent response compression")
	flag.BoolVar(&debugMode, "debug", false, "Enable debug mode (logs requests)")
	flag.StringVar(&listenAddr, "listen-addr", ":8080", "TCP address to listen to")
	flag.StringVar(&postgresURI, "postgres-uri", "", "* Postgres connection URI")
	flag.StringVar(&seaweedMasterURI, "seaweed-master-uri", "", "* SeaweedFS master URI")
	flag.Parse()

	// Overwrite configuration from environment
	if os.Getenv("COMPRESS") != "" {
		val, err := strconv.ParseBool(os.Getenv("COMPRESS"))
		if err == nil {
			compress = val
		}
	}
	if os.Getenv("DEBUG") != "" {
		val, err := strconv.ParseBool(os.Getenv("DEBUG"))
		if err == nil {
			debugMode = val
		}
	}
	if os.Getenv("LISTEN_ADDR") != "" {
		listenAddr = os.Getenv("LISTEN_ADDR")
	}
	if os.Getenv("POSTGRES_URI") != "" {
		postgresURI = os.Getenv("POSTGRES_URI")
	}
	if os.Getenv("SEAWEED_MASTER_URI") != "" {
		seaweedMasterURI = os.Getenv("SEAWEED_MASTER_URI")
	}

	// Enable debug mode
	if debugMode {
		log.SetLevel(log.DebugLevel)
	}

	// Print configuration variables to debug
	log.WithFields(log.Fields{
		"compress":         compress,
		"debugMode":        debugMode,
		"listenAddr":       listenAddr,
		"postgresURI":      postgresURI,
		"seaweedMasterURI": seaweedMasterURI,
	}).Debug("retrieved configuration variables from args and env")
}

// db holds the current PostgreSQL database connection.
var db *sql.DB

// seaweed client to use for fetching files from the SeaweedFS cluster.
var seaweed *weed.Seaweed

func main() {
	var err error

	// Attempt to connect to SeaweedFS master
	seaweed = weed.New(seaweedMasterURI, time.Second*5)
	err = seaweed.Ping()
	if err != nil {
		log.WithField("err", err).Fatal("failed to ping SeaweedFS master")
		return
	}

	// Connect to PostgreSQL database
	if postgresURI == "" {
		log.Fatal("postgresURI is required")
		return
	}
	db, err = sql.Open("postgres", postgresURI)
	if err != nil {
		log.WithField("err", err).Fatal("failed to open database connection")
		return
	}

	// Set redirect template values
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
	if compress {
		h = fasthttp.CompressHandler(h)
	}
	log.Info("Attempting to listen on " + listenAddr)
	server := &fasthttp.Server{
		Handler:                       h,
		Name:                          "whats-this/cdn-origin/1.0.0",
		ReadBufferSize:                1024 * 6, // 6 KB
		ReadTimeout:                   time.Minute * 30,
		WriteTimeout:                  time.Minute * 30,
		GetOnly:                       true,
		LogAllErrors:                  debugMode,
		DisableHeaderNamesNormalizing: true,
		Logger: log.New(),
	}
	if err := server.ListenAndServe(listenAddr); err != nil {
		log.WithField("err", err).Fatal("error in ListenAndServe")
	}
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	// Wrapped in if to prevent unnecessary memory allocations
	if debugMode {
		log.WithFields(log.Fields{
			"connRequestNumber": ctx.ConnRequestNum(),
			// "connTime":          ctx.ConnTime(),
			"method":      string(ctx.Method()),
			"path":        string(ctx.Path()),
			"queryString": ctx.QueryArgs(),
			"remoteIP":    ctx.RemoteIP(),
			"requestURI":  string(ctx.RequestURI()),
			// "time":              ctx.Time(),
			// "userAgent":         string(ctx.UserAgent()),
		}).Debug("request received")
	}

	// Fetch object from database
	var backend_file_id sql.NullString
	var content_type sql.NullString
	var long_url sql.NullString
	var object_type int
	err := db.QueryRow(`SELECT backend_file_id, content_type, long_url, "type" FROM objects WHERE bucket='0' AND "key"=$1 LIMIT 1`, string(ctx.Path())).Scan(&backend_file_id, &content_type, &long_url, &object_type)
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
			log.Warn("encountered file object with NULL backend_file_id")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
		}
		if content_type.Valid {
			ctx.SetContentType(content_type.String)
		} else {
			ctx.SetContentType("application/octet-stream")
		}

		statusCode, err := seaweed.Get(backend_file_id.String, "", ctx)
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

	case 1: // short_url
		if !long_url.Valid {
			log.Warn("encountered short_url object with NULL long_url")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
		}

		if ctx.QueryArgs().Has("preview") {
			buf := new(bytes.Buffer)
			err := redirectPreviewHTMLTemplate.Execute(buf, long_url.String)
			if err != nil {
				log.WithFields(log.Fields{
					"err":      err,
					"long_url": long_url.String,
				}).Warn("failed to generate redirect preview HTML to send to client")
				ctx.SetContentType("text/plain; charset=utf8")
				fmt.Fprintf(ctx, "Failed to generate preview page, long URL: %s", long_url.String)
				return
			}
			b := buf.Bytes()
			s := *(*string)(unsafe.Pointer(&b))
			ctx.SetContentType("text/html; charset=utf8")
			fmt.Fprint(ctx, s)
		} else {
			buf := new(bytes.Buffer)
			err := redirectHTMLTemplate.Execute(buf, long_url.String)
			if err != nil {
				log.WithFields(log.Fields{
					"err":      err,
					"long_url": long_url.String,
				}).Warn("failed to generate redirect HTML to send to client")
				ctx.SetContentType("text/plain; charset=utf8")
				fmt.Fprintf(ctx, "Failed to generate HTML fallback page, long URL: %s", long_url.String)
				return
			}
			b := buf.Bytes()
			s := *(*string)(unsafe.Pointer(&b))
			ctx.SetContentType("text/html; charset=utf8")
			fmt.Fprint(ctx, s)
		}
	}
}
