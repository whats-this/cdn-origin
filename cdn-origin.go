package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"net"
	"strings"
	"time"

	"github.com/whats-this/cdn-origin/metrics"
	"github.com/whats-this/cdn-origin/weed"

	log "github.com/Sirupsen/logrus"
	_ "github.com/lib/pq"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/valyala/fasthttp"
)

// version is the current version of cdn-origin.
const version = "0.4.0"

// Default SeaweedFS fetch query parameters.
const defaultSeaweedFSQueryParameters = ""

// HTTP header strings.
const (
	accept   = "accept"
	host     = "host"
	location = "Location"
)

// redirectHTML is the html/template template for generating redirect HTML.
const redirectHTML = `<html><head><meta charset="UTF-8" /><meta http-equiv=refresh content="0; url={{.}}" /><script type="text/javascript">window.location.href="{{.}}"</script><title>Redirect</title></head><body><p>If you are not redirected automatically, click <a href="{{.}}">here</a> to go to the destination.</p></body></html>`

var redirectHTMLTemplate *template.Template

// redirectPreviewHTML is the html/template template for generating redirect preview HTML.
const redirectPreviewHTML = `<html><head><meta charset="UTF-8" /><title>Redirect Preview</title></head><body><p>This link goes to <code>{{.}}</code>. If you would like to visit this link, click <a href="{{.}}">here</a> to go to the destination.</p></body></html>`

var redirectPreviewHTMLTemplate *template.Template

func init() {
	// Read in configuration
	flags := pflag.NewFlagSet("cdn-origin", pflag.ExitOnError)

	// database.connectionURL* (string): PostgreSQL connection URL
	flags.String("database-connection-url", "", "* PostgreSQL connection URL")
	viper.BindPFlag("database.connectionURL", flags.Lookup("database-connection-url"))
	viper.BindEnv("database.connectionURL", "DATABASE_CONNECTION_URL")

	// debug (bool=false): enable debug mode (logs requests and prints other information)
	flags.Bool("debug", false, "Enable debug mode (logs requests and prints other information)")
	viper.BindPFlag("debug", flags.Lookup("debug"))
	viper.BindEnv("debug", "DEBUG")

	// http.compressResponse (bool=false): enable transparent response compression
	flags.Bool("compress-response", false, "Enable transparent response compression")
	viper.BindPFlag("http.compressResponse", flags.Lookup("compress-response"))
	viper.BindEnv("http.compressResponse", "HTTP_COMPRESS_RESPONSE")

	// http.listenAddress (string=":8080"): TCP address to listen to for HTTP requests
	flags.String("listen-address", ":8080", "TCP address to listen to for HTTP requests")
	viper.BindPFlag("http.listenAddress", flags.Lookup("listen-address"))
	viper.BindEnv("http.listenAddress", "HTTP_LISTEN_ADDRESS")

	// http.trustProxy (bool=false): trust X-Forwarded-For header from proxy
	flags.Bool("trust-proxy", false, "Trust X-Forwarded-For header from proxy")
	viper.BindPFlag("http.trustProxy", flags.Lookup("trust-proxy"))
	viper.BindEnv("http.trustProxy", "HTTP_TRUST_PROXY")

	// metrics.enable (bool=false): enable anonymized request recording (country code, hostname, object type, status
	// code)
	flags.Bool("enable-metrics", false,
		"Enable anonymized request recording (status code, country code, object type, hostname)")
	viper.BindPFlag("metrics.enable", flags.Lookup("enable-metrics"))
	viper.BindEnv("metrics.enable", "ENABLE_METRICS")

	// metrics.elasticURL (string): Elasticsearch URL to connect to for metrics, required if metrics.enable == true
	flags.String("elastic-url", "", "Elastic URL to connect to for metrics")
	viper.BindPFlag("metrics.elasticURL", flags.Lookup("elastic-url"))
	viper.BindEnv("metrics.elasticURL", "ELASTIC_URL")

	// metrics.enableHostnameWhitelist (bool=false): enable hostname whitelist for metrics
	// (metrics.hostnameWhitelist)
	flags.Bool("metrics-enable-whitelist", false, "Enable hostname whitelist for metrics")
	viper.BindPFlag("metrics.enableHostnameWhitelist", flags.Lookup("metrics-enable-whitelist"))
	viper.BindEnv("metrics.enableHostnameWhitelist", "METRICS_ENABLE_WHITELIST")

	// metrics.hostnameWhitelist (string[]=[]): hostnames to whitelist for metrics (config file only)

	// metrics.maxmindDBLocation (string): location of MaxMind GeoLite2 Country database file (.mmdb) on disk, if
	// empty country codes will not be recorded by metrics collector
	flags.String("maxmind-db-location", "", "Location of MaxMind GeoLite2 Country database on disk")
	viper.BindPFlag("metrics.maxmindDBLocation", flags.Lookup("maxmind-db-location"))
	viper.BindEnv("metrics.maxmindDBLocation", "MAXMIND_DB_LOCATION")

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
			log.WithError(err).Fatal("failed to read in configuration")
		}
	}

	// Enable debug mode
	if viper.GetBool("debug") {
		log.SetLevel(log.DebugLevel)
	}

	// Print configuration variables to debug
	log.WithFields(log.Fields{
		"database.connectionURL":          viper.GetString("database.connectionURL"),
		"debug":                           viper.GetBool("debug"),
		"http.compressResponse":           viper.GetBool("http.compressResponse"),
		"http.listenAddress":              viper.GetString("http.listenAddress"),
		"metrics.enable":                  viper.GetBool("metrics.enable"),
		"metrics.elasticURL":              viper.GetString("metrics.elasticURL"),
		"len(metrics.hostnameWhitelist)":  len(viper.GetStringSlice("metrics.hostnameWhitelist")),
		"metrics.enableHostnameWhitelist": viper.GetString("metrics.enableHostnameWhitelist"),
		"seaweed.masterURL":               viper.GetString("seaweed.masterURL"),
	}).Debug("retrieved configuration")

	// Ensure required configuration variables are set
	if viper.GetString("database.connectionURL") == "" {
		log.Fatal("database.connectionURL is required")
	}
	if viper.GetString("seaweed.masterURL") == "" {
		log.Fatal("seaweed.masterURL is required")
	}
	if viper.GetBool("metrics.enable") && viper.GetString("metrics.elasticURL") == "" {
		log.Fatal("metrics.elasticURL is required when metrics are enabled")
	}

	// Parse redirect templates
	var err error
	redirectHTMLTemplate, err = template.New("redirectHTML").Parse(redirectHTML)
	if err != nil {
		log.WithError(err).Fatal("failed to parse redirectHTML template")
	}
	redirectPreviewHTMLTemplate, err = template.New("redirectPreviewHTML").Parse(redirectPreviewHTML)
	if err != nil {
		log.WithError(err).Fatal("failed to parse redirectPreviewHTML template")
	}
}

var db *sql.DB
var collector *metrics.Collector
var seaweed *weed.Seaweed

func main() {
	var err error

	// Attempt to connect to SeaweedFS master
	seaweed = weed.New(viper.GetString("seaweed.masterURL"), time.Second*5)
	err = seaweed.Ping()
	if err != nil {
		log.WithError(err).Fatal("failed to ping SeaweedFS master")
	}

	// Connect to PostgreSQL database
	// TODO: abstractify database connection
	// TODO: create table if it doesn't exist
	db, err = sql.Open("postgres", viper.GetString("database.connectionURL"))
	if err != nil {
		log.WithError(err).Fatal("failed to open database connection")
	}

	// Setup metrics collector
	if viper.GetBool("metrics.enable") {
		hostnameWhitelist := []string{}
		if viper.GetBool("metrics.enableHostnameWhitelist") {
			switch w := viper.Get("metrics.hostnameWhitelist").(type) {
			case []interface{}:
				for _, s := range w {
					hostnameWhitelist = append(hostnameWhitelist, strings.TrimSpace(fmt.Sprint(s)))
				}
				break
			default:
				log.Fatal("metrics.hostnameWhitelist is not an array")
			}
		}
		collector, err = metrics.New(
			viper.GetString("metrics.elasticURL"),
			viper.GetString("metrics.maxmindDBLocation"),
			viper.GetBool("metrics.enableHostnameWhitelist"),
			hostnameWhitelist,
		)
		if err != nil {
			log.WithError(err).Fatal("failed to setup metrics collector")
		}
	}

	// Launch server
	h := requestHandler
	if viper.GetBool("http.compressResponse") {
		h = fasthttp.CompressHandler(h)
	}
	log.Info("Attempting to listen on " + viper.GetString("http.listenAddress"))
	server := &fasthttp.Server{
		Handler:                       h,
		Name:                          "whats-this/cdn-origin v" + version,
		ReadBufferSize:                1024 * 6, // 6 KB
		ReadTimeout:                   time.Minute * 30,
		WriteTimeout:                  time.Minute * 30,
		GetOnly:                       true, // TODO: OPTIONS/HEAD requests
		LogAllErrors:                  log.GetLevel() == log.DebugLevel,
		DisableHeaderNamesNormalizing: false,
		Logger: log.New(),
	}
	if err := server.ListenAndServe(viper.GetString("http.listenAddress")); err != nil {
		log.WithError(err).Fatal("error in server.ListenAndServe")
	}
}

func recordMetrics(ctx *fasthttp.RequestCtx, objectType string) {
	if !viper.GetBool("metrics.enable") {
		return
	}

	var remoteIP net.IP
	if viper.GetBool("http.trustProxy") {
		ipString := string(ctx.Request.Header.Peek("X-Forwarded-For"))
		remoteIP = net.ParseIP(strings.Split(ipString, ",")[0])
	} else {
		remoteIP = ctx.RemoteIP()
	}

	hostBytes := ctx.Request.Header.Peek(host)
	if len(hostBytes) != 0 {
		go func() {
			// Check hostname
			hostStr, isValid := collector.MatchHostname(string(hostBytes))
			if !isValid {
				return
			}

			// Get country code of visitor
			countryCode, err := collector.GetCountryCode(remoteIP)
			if err != nil {
				// Don't log the error here, it might contain an IP address
				log.Warn("failed to get country code for IP, omitting from record")
			}

			record := metrics.GetRecord()
			record.CountryCode = countryCode
			record.Hostname = hostStr
			record.ObjectType = objectType
			record.StatusCode = ctx.Response.StatusCode()
			err = collector.Put(record)
			if err != nil {
				log.WithError(err).Warn("failed to collect record")
			}
			log.Debug("successfully collected metrics")
		}()
	}
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	metricsObjectType := ""

	// Log requests in debug mode, wrapped in an if statement to prevent unnecessary memory allocations
	if log.GetLevel() == log.DebugLevel {
		log.WithFields(log.Fields{
			"connRequestNumber": ctx.ConnRequestNum(),
			"method":            string(ctx.Method()),
			"queryString":       ctx.QueryArgs(),
			"remoteIP":          ctx.RemoteIP(),
			"requestURI":        string(ctx.RequestURI()),
		}).Debug("request received")
	}

	// Fetch object from database
	var backendFileID sql.NullString
	var contentType sql.NullString
	var destURL sql.NullString
	var objectType int
	err := db.QueryRow(
		`SELECT backend_file_id, content_type, dest_url, "type" FROM objects WHERE bucket_key=$1 LIMIT 1`,
		fmt.Sprintf("public%s", ctx.Path()),
	).Scan(&backendFileID, &contentType, &destURL, &objectType)
	switch {
	case err == sql.ErrNoRows:
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetContentType("text/plain; charset=utf8")
		fmt.Fprintf(ctx, "404 Not Found: %s", ctx.Path())
		recordMetrics(ctx, metricsObjectType)
		return
	case err != nil:
		log.WithError(err).Error("failed to run SELECT query on database")
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetContentType("text/plain; charset=utf8")
		fmt.Fprint(ctx, "500 Internal Server Error")
		ctx.SetUserValue("object_type", "file")
		recordMetrics(ctx, metricsObjectType)
		return
	}

	switch objectType {
	case 0: // file
		metricsObjectType = "file"

		// Get object from SeaweedFS and write to response
		if !backendFileID.Valid {
			log.WithField("bucket_key", string(ctx.Path())).Warn("found file object with NULL backend_file_id")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
			recordMetrics(ctx, metricsObjectType)
			return
		}

		// TODO: ?thumbnail query parameter for images
		statusCode, contentSize, err := seaweed.Get(ctx, backendFileID.String, defaultSeaweedFSQueryParameters)
		if err != nil {
			log.WithError(err).Warn("failed to retrieve file from SeaweedFS volume server")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
			recordMetrics(ctx, metricsObjectType)
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
			recordMetrics(ctx, metricsObjectType)
			return
		}

		if contentType.Valid {
			ctx.SetContentType(contentType.String)
		} else {
			ctx.SetContentType("application/octet-stream")
		}
		ctx.Response.Header.SetContentLength(contentSize)
		recordMetrics(ctx, metricsObjectType)

	case 1: // redirect
		metricsObjectType = "redirect"

		if !destURL.Valid {
			log.Warn("encountered redirect object with NULL dest_url")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprint(ctx, "500 Internal Server Error")
			recordMetrics(ctx, metricsObjectType)
			return
		}

		previewMode := ctx.QueryArgs().Has("preview")
		var err error
		if previewMode {
			err = redirectPreviewHTMLTemplate.Execute(ctx, destURL.String)
		} else {
			err = redirectHTMLTemplate.Execute(ctx, destURL.String)
		}

		if err != nil {
			log.WithFields(log.Fields{
				"error":    err,
				"dest_url": destURL.String,
				"preview":  ctx.QueryArgs().Has("preview"),
			}).Warn("failed to generate HTML redirect page to send to client")
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprintf(ctx, "Failed to generate HTML redirect page, destination URL: %s", destURL.String)
			recordMetrics(ctx, metricsObjectType)
			return
		}

		ctx.SetContentType("text/html; charset=ut8")
		if !previewMode {
			ctx.SetStatusCode(fasthttp.StatusFound)
			ctx.Response.Header.Set(location, destURL.String)
		}
		recordMetrics(ctx, metricsObjectType)
	}
}
