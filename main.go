package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"owo.codes/whats-this/cdn-origin/lib/db"
	"owo.codes/whats-this/cdn-origin/lib/metrics"
	"owo.codes/whats-this/cdn-origin/lib/thumbnailer"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/valyala/fasthttp"
)

// Build config
const (
	configLocationUnix = "/etc/whats-this/cdn-origin/config.toml"
	version            = "0.7.0"
)

// readCloserBuffer is a *bytes.Buffer that implements io.ReadCloser.
type readCloserBuffer struct {
	*bytes.Buffer
}

func (b *readCloserBuffer) Close() error {
	return nil
}

var _ io.ReadCloser = &readCloserBuffer{}

// redirectHTML is the html/template template for generating redirect HTML.
const redirectHTML = `<html><head><meta charset="UTF-8" /><meta http-equiv=refresh content="0; url={{.}}" /><script type="text/javascript">window.location.href="{{.}}"</script><title>Redirect</title></head><body><p>If you are not redirected automatically, click <a href="{{.}}">here</a> to go to the destination.</p></body></html>`

var redirectHTMLTemplate *template.Template

// redirectPreviewHTML is the html/template template for generating redirect preview HTML.
const redirectPreviewHTML = `<html><head><meta charset="UTF-8" /><title>Redirect Preview</title></head><body><p>This link goes to <code>{{.}}</code>. If you would like to visit this link, click <a href="{{.}}">here</a> to go to the destination.</p></body></html>`

var redirectPreviewHTMLTemplate *template.Template

// printConfiguration iterates through a configuration map[string]interface{}
// and prints out all of the values in alphabetical order. Configuration keys
// are printed with dot notation.
func printConfiguration(prefix string, config map[string]interface{}) {
	keys := make([]string, len(config))
	i := 0
	for k := range config {
		keys[i] = k
		i++
	}
	sort.Strings(keys)

	for _, k := range keys {
		if v, ok := config[k].(map[string]interface{}); ok {
			printConfiguration(fmt.Sprintf("%s%s.", prefix, k), v)
		} else {
			fmt.Printf("%s%s: %+v\n", prefix, k, config[k])
		}
	}
}

func init() {
	// Flag configuration
	flags := pflag.NewFlagSet("whats-this-cdn-origin", pflag.ExitOnError)
	flags.IntP("log-level", "l", 1, "Set zerolog logging level (5=panic, 4=fatal, 3=error, 2=warn, 1=info, 0=debug)")
	configFile := flags.StringP("config-file", "c", configLocationUnix,
		fmt.Sprintf("Path to configuration file, defaults to %s", configLocationUnix))
	printConfig := flags.BoolP("print-config", "p", false, "Prints configuration and exits")
	flags.Parse(os.Args)

	// Configuration defaults
	viper.SetDefault("database.objectBucket", "public")
	viper.SetDefault("http.compressResponse", false)
	viper.SetDefault("http.listenAddress", ":49544")
	viper.SetDefault("http.trustProxy", false)
	viper.BindPFlag("log.level", flags.Lookup("log-level")) // default is 1 (info)
	viper.SetDefault("metrics.enable", false)
	viper.SetDefault("metrics.enableHostnameWhitelist", false)

	// Load configuration file
	viper.SetConfigType("toml")
	file, err := os.Open(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open configuration file (%s) for reading: %s", *configFile, err.Error())
		os.Exit(1)
		return
	}
	err = viper.ReadConfig(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse configuration file (%s): %s", *configFile, err.Error())
		os.Exit(1)
		return
	}
	file.Close()

	// Configure logger
	zerolog.TimeFieldFormat = ""
	if lvl := viper.GetInt("log.level"); 0 <= lvl && lvl <= 5 {
		zerolog.SetGlobalLevel(zerolog.Level(lvl))
	} else {
		viper.Set("log.level", 1)
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Warn().Int("log.level", lvl).Msg("Invalid log level, defaulting to 1 (info)")
	}
	log.Debug().Uint8("level", uint8(zerolog.GlobalLevel())).Msg("Set logger level")

	// Print configuration variables in alphabetical order
	if *printConfig {
		log.Info().Msg("Printing configuration values to Stdout")
		settings := viper.AllSettings()
		printConfiguration("", settings)
		os.Exit(0)
		return
	}

	// Ensure required configuration variables are set
	if viper.GetString("database.connectionURL") == "" {
		log.Fatal().Msg("Configuration: database.connectionURL is required")
	}
	if viper.GetString("database.objectBucket") == "" {
		log.Fatal().Msg("Configuration: database.objectBucket is required")
	}
	if viper.GetBool("metrics.enable") && viper.GetBool("metrics.enableHostnameWhitelist") && len(viper.GetStringSlice("metrics.hostnameWhitelist")) == 0 {
		log.Fatal().Msg("Configuration: metrics.hostnameWhitelist is required when metrics and hostname whitelist is enabled")
	}
	if viper.GetString("http.listenAddress") == "" {
		log.Fatal().Msg("Configuration: http.listenAddress is required")
	}
	if viper.GetString("files.storageLocation") == "" {
		log.Fatal().Msg("Configuration: files.storageLocation is required")
	}
	if viper.GetBool("thumbnails.enable") && viper.GetBool("thumbnails.cacheEnable") && viper.GetString("thumbnails.cacheLocation") == "" {
		log.Fatal().Msg("thumbnails.cacheLocation is required when thumbnails and thumbnails cache is enabled")
	}

	// Parse redirect templates
	redirectHTMLTemplate, err = template.New("redirectHTML").Parse(redirectHTML)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse redirectHTML template")
	}
	redirectPreviewHTMLTemplate, err = template.New("redirectPreviewHTML").Parse(redirectPreviewHTML)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to parse redirectPreviewHTML template")
	}
}

var collector *metrics.Collector
var thumbnailCache *thumbnailer.ThumbnailCache

func main() {
	// Connect to PostgreSQL database
	err := db.Connect("postgres", viper.GetString("database.connectionURL"))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open database connection")
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
				log.Fatal().Msg("metrics.hostnameWhitelist is not an array")
			}
		}
		collector, err = metrics.New(
			viper.GetString("metrics.elasticURL"),
			viper.GetString("metrics.maxmindDBLocation"),
			viper.GetBool("metrics.enableHostnameWhitelist"),
			hostnameWhitelist,
		)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to setup metrics collector")
		}
	}

	// Setup thumbnail cache
	if viper.GetBool("thumbnails.enable") && viper.GetBool("thumbnails.cacheEnable") {
		thumbnailCache = thumbnailer.NewThumbnailCache(viper.GetString("thumbnails.cacheLocation"))
	}

	// Launch server
	h := requestHandler
	if viper.GetBool("http.compressResponse") {
		h = fasthttp.CompressHandler(h)
	}
	listenAddress := viper.GetString("http.listenAddress")
	log.Info().Str("listenAddress", listenAddress).Msg("Starting HTTP server")
	server := &fasthttp.Server{
		Handler:                       h,
		Name:                          "whats-this/cdn-origin v" + version,
		ReadBufferSize:                1024 * 6, // 6 KB
		ReadTimeout:                   time.Minute * 30,
		WriteTimeout:                  time.Minute * 30,
		GetOnly:                       true, // TODO: OPTIONS/HEAD requests
		DisableHeaderNamesNormalizing: false,
	}
	if err := server.ListenAndServe(listenAddress); err != nil {
		log.Fatal().Err(err).Msg("error in server.ListenAndServe")
	}
}

func recordMetrics(ctx *fasthttp.RequestCtx) {
	if !viper.GetBool("metrics.enable") {
		return
	}

	// Get object type
	objectType := ""
	if v, ok := ctx.UserValue("object_type").(string); ok {
		objectType = v
	}

	// Determine remote IP
	var remoteIP net.IP
	if viper.GetBool("http.trustProxy") {
		ipString := string(ctx.Request.Header.Peek("X-Forwarded-For"))
		remoteIP = net.ParseIP(strings.Split(ipString, ",")[0])
	} else {
		remoteIP = ctx.RemoteIP()
	}

	// Anonymize host string and send record to Elasticsearch
	hostBytes := ctx.Request.Header.Peek("Host")
	statusCode := ctx.Response.StatusCode()
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
				log.Warn().Msg("failed to get country code for IP, omitting from record")
			}

			record := metrics.GetRecord()
			record.CountryCode = countryCode
			record.Hostname = hostStr
			record.ObjectType = objectType
			record.StatusCode = statusCode
			err = collector.Put(record)
			if err != nil {
				log.Warn().Err(err).Msg("failed to collect record")
				return
			}
			log.Debug().Msg("successfully collected metrics")
		}()
	}
}

func requestHandler(ctx *fasthttp.RequestCtx) {
	defer recordMetrics(ctx)

	// Fetch object from database
	key := string(ctx.Path()[1:])
	object, err := db.SelectObjectByBucketKey(viper.GetString("database.objectBucket"), key)
	switch {
	case err == sql.ErrNoRows:
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetContentType("text/plain; charset=utf8")
		fmt.Fprintf(ctx, "404 Not Found: %s", ctx.Path())
		return
	case err != nil:
		log.Error().Err(err).Msg("failed to run SELECT query on database")
		internalServerError(ctx)
		return
	}

	switch object.ObjectType {
	case 0: // file
		ctx.SetUserValue("object_type", "file")

		// Thumbnails
		if viper.GetBool("thumbnails.enable") && ctx.QueryArgs().Has("thumbnail") {
			thumbnailKey := *object.MD5Hash
			if !thumbnailer.AcceptedMIMEType(*object.ContentType) || thumbnailKey == "" {
				ctx.SetStatusCode(fasthttp.StatusNotFound)
				ctx.SetContentType("text/plain; charset=utf8")
				fmt.Fprintf(ctx, "404 Not Found: %s?thumbnail (cannot generate thumbnail)", ctx.Path())
				return
			}

			// Get thumbnail
			var thumb io.ReadCloser
			if viper.GetBool("thumbnails.cacheEnable") {
				thumb, err = thumbnailCache.GetThumbnail(thumbnailKey)
				if thumb != nil {
					defer thumb.Close()
				}
				if err == thumbnailer.NoCachedCopy {
					fPath := filepath.Join(viper.GetString("files.storageLocation"), key)
					file, err := os.Open(fPath)
					if file != nil {
						defer file.Close()
					}
					if err != nil {
						log.Warn().Err(err).Msg("failed to open original file to generate thumbnail")
						internalServerError(ctx)
						return
					}
					err = thumbnailCache.Transform(thumbnailKey, file)
					if err == thumbnailer.InputTooLarge {
						ctx.SetStatusCode(fasthttp.StatusNotFound)
						ctx.SetContentType("text/plain; charset=utf8")
						fmt.Fprintf(ctx, "404 Not Found: %s?thumbnail (cannot generate thumbnail)", ctx.Path())
						return
					} else if err != nil {
						log.Warn().Err(err).Msg("failed to generate new thumbnail")
						internalServerError(ctx)
						return
					}
					thumb, err = thumbnailCache.GetThumbnail(thumbnailKey)
					if thumb != nil {
						defer thumb.Close()
					}
					if err != nil {
						log.Warn().Err(err).Msg("failed to get thumbnail from cache")
						internalServerError(ctx)
						return
					}
				} else if err != nil {
					log.Warn().Err(err).Msg("failed to get thumbnail from cache")
					internalServerError(ctx)
					return
				}
			} else {
				fPath := filepath.Join(viper.GetString("files.storageLocation"), key)
				file, err := os.Open(fPath)
				if file != nil {
					defer file.Close()
				}
				if err != nil {
					log.Warn().Err(err).Msg("failed to open original file to generate thumbnail")
					internalServerError(ctx)
					return
				}
				thumbR, err := thumbnailer.Transform(file)
				if err == thumbnailer.InputTooLarge {
					ctx.SetStatusCode(fasthttp.StatusNotFound)
					ctx.SetContentType("text/plain; charset=utf8")
					fmt.Fprintf(ctx, "404 Not Found: %s?thumbnail (cannot generate thumbnail)", ctx.Path())
					return
				} else if err != nil {
					log.Warn().Err(err).Msg("failed to generate new thumbnail")
					internalServerError(ctx)
					return
				}
				// Turn the *bytes.Buffer from thumbnailer.Transform into a fake io.ReadCloser.
				thumb = &readCloserBuffer{thumbR}
			}

			// Send response
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetContentType("image/jpeg")
			ctx.Response.Header.Set("Content-Disposition", fmt.Sprintf(`filename="%s.thumbnail.jpeg"`, key))
			_, err = io.Copy(ctx, thumb)
			if err != nil {
				log.Warn().Err(err).Msg("failed to send thumbnail response")
				ctx.Response.Header.Del("Content-Disposition")
				internalServerError(ctx)
			}
			return
		}

		// Serve file to client
		fPath := filepath.Join(viper.GetString("files.storageLocation"), key)
		ctx.SetStatusCode(fasthttp.StatusOK)
		if object.ContentType != nil {
			ctx.SetContentType(*object.ContentType)
		} else {
			ctx.SetContentType("application/octet-stream")
		}
		fasthttp.ServeFileUncompressed(ctx, fPath)

	case 1: // redirect
		ctx.SetUserValue("object_type", "redirect")

		if object.DestURL == nil {
			log.Warn().Str("key", key).Msg("encountered redirect object with NULL dest_url")
			internalServerError(ctx)
			return
		}

		previewMode := ctx.QueryArgs().Has("preview")
		var err error
		if previewMode {
			err = redirectPreviewHTMLTemplate.Execute(ctx, object.DestURL)
		} else {
			err = redirectHTMLTemplate.Execute(ctx, object.DestURL)
		}

		if err != nil {
			log.Warn().Err(err).
				Str("dest_url", *object.DestURL).
				Bool("preview", ctx.QueryArgs().Has("preview")).
				Msg("failed to generate HTML redirect page to send to client")
			ctx.SetContentType("text/plain; charset=utf8")
			fmt.Fprintf(ctx, "Failed to generate HTML redirect page, destination URL: %s", *object.DestURL)
			return
		}

		ctx.SetContentType("text/html; charset=ut8")
		if !previewMode {
			ctx.SetStatusCode(fasthttp.StatusFound)
			ctx.Response.Header.Set("Location", *object.DestURL)
		} else {
			ctx.SetStatusCode(fasthttp.StatusOK)
		}

	case 2: // tombstone
		ctx.SetUserValue("object_type", "tombstone")

		// Send 410 gone response
		ctx.SetStatusCode(fasthttp.StatusGone)
		ctx.SetContentType("text/plain; charset=utf8")
		reason := "no reason specified"
		if object.DeleteReason != nil && *object.DeleteReason != "" {
			reason = *object.DeleteReason
		}
		fmt.Fprintf(ctx, "410 Gone: %s\n\nReason: %s", ctx.Path(), reason)
	}
}

// internalServerError returns a 500 Internal Server Response.
func internalServerError(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	ctx.SetContentType("text/plain; charset=utf8")
	fmt.Fprint(ctx, "500 Internal Server Error")
}
