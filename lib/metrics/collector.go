package metrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/oschwald/maxminddb-golang"
	"github.com/rs/zerolog/log"
	"gopkg.in/olivere/elastic.v5"
	"gopkg.in/olivere/elastic.v5/config"
)

// mapping is the default mapping to use when creating the index if it doesn't exist. This JSON data is also maintained
// in `./mapping.elasticsearch.json`.
const mapping = `
{
  "settings": {
    "number_of_shards": 1
  },

  "mappings": {
    "request": {
      "properties": {
        "country_code": {
          "type": "keyword",
          "ignore_above": 2,
          "index": true
        },
        "hostname": {
          "type": "keyword",
          "ignore_above": 30,
          "index": true
        },
        "object_type": {
          "type": "keyword",
          "ignore_above": 30,
          "index": true
        },
        "status_code": {
          "type": "short",
          "index": true
        },

        "@timestamp": {
          "type": "date",
          "index": true
        }
      }
    }
  }
}`

// The default `@timestamp` pipeline. Sets the `@timestamp` field to the ingest timestamp (date type). This JSON data is
// also maintained in `./timestampPipeline.elasticsearch.json`.
const timestampPipeline = `
{
  "description": "Stores the ingest timestamp as a date field in the document.",
  "processors": [
    {
      "set": {
        "field": "@timestamp",
        "value": "{{_ingest.timestamp}}"
      }
    },
    {
      "date": {
        "field": "@timestamp",
        "target_field": "@timestamp",
        "formats": ["EEE MMM d HH:mm:ss z yyyy"]
      }
    }
  ]
}`

// Collector collects request metadata and sends it to Elasticsearch.
type Collector struct {
	ctx     context.Context
	elastic *elastic.Client
	index   string

	geoIPDatabase *maxminddb.Reader

	enableHostnameWhitelist bool
	hostnameWhitelist       *treeNode
}

// New creates a new Elasticsearch connection and returns a Collector using that connection.
func New(elasticURL string, maxmindLoc string, enableHostnameWhitelist bool, hostnameWhitelist []string) (*Collector, error) {
	// Parse elasticURL
	cfg, err := config.Parse(elasticURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse elasticURL: %s", err)
	}
	if cfg.Index == "" {
		log.Info().Msg(`empty index name in elasticURL, using "cdn-origin_requests"`)
		cfg.Index = "cdn-origin_requests"
	}

	// Create client and ping Elasticsearch server
	client, err := elastic.NewClientFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create elastic client: %s", err)
	}
	ctx := context.Background()
	info, code, err := client.Ping(cfg.URL).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ping Elasticsearch server: %s", err)
	}
	log.Debug().Int("statusCode", code).Str("version", info.Version.Number).Msg("elasticsearch ping success")

	// Check if the index exists or create it
	exists, err := client.IndexExists(cfg.Index).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to determine if the index exists: %s", err)
	}
	if !exists {
		log.Info().Str("index", cfg.Index).Msg("creating Elasticsearch index")
		createIndex, err := client.CreateIndex(cfg.Index).
			BodyString(mapping).
			Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create missing index on Elasticsearchr: %s", err)
		}
		if !createIndex.Acknowledged {
			return nil, errors.New("failed to create missing index on Elasticsearch: no acknowledged")
		}
	}

	// Check if the timestamp pipeline exists or create it
	pipelines, err := elastic.NewIngestGetPipelineService(client).
		Id("timestamp").
		Do(ctx)
	if err != nil && !strings.Contains(err.Error(), "404") {
		return nil, fmt.Errorf("failed to determine if the timestamp pipeline exists: %s", err)
	}
	if len(pipelines) == 0 {
		log.Info().Str("pipeline", "timestamp").Msg("creating Elasticsearch ingest pipeline")
		putPipeline, err := elastic.NewIngestPutPipelineService(client).
			Id("timestamp").
			BodyString(timestampPipeline).
			Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to put missing pipeline on Elasticsearch: %s", err)
		}
		if !putPipeline.Acknowledged {
			return nil, errors.New("failed to put missing pipeline on Elasticsearch: not acknowledged")
		}
	}

	// Create Maxmind GeoLite2 Country database reader
	var geoIPDatabase *maxminddb.Reader
	if maxmindLoc != "" {
		geoIPDatabase, err = maxminddb.Open(maxmindLoc)
		if err != nil {
			return nil, fmt.Errorf("failed to open MaxMind GeoLite2 Country database: %s", err)
		}
	}

	// Construct hostname whitelist *treeNode
	var hostnameWhitelistTree *treeNode
	if enableHostnameWhitelist {
		hostnameWhitelistTree = parseWhitelistSlice(hostnameWhitelist)
	}

	// Create Collector
	return &Collector{
		ctx:                     ctx,
		elastic:                 client,
		index:                   cfg.Index,
		geoIPDatabase:           geoIPDatabase,
		enableHostnameWhitelist: enableHostnameWhitelist,
		hostnameWhitelist:       hostnameWhitelistTree,
	}, nil
}

// Put indexes a record in the Elasticsearch server.
func (c *Collector) Put(record *Record) error {
	_, err := c.elastic.Index().
		Index(c.index).
		Type("request").
		Pipeline("timestamp").
		BodyJson(record).
		Do(c.ctx)
	if err != nil {
		return fmt.Errorf("failed to index record: %s", err)
	}
	return nil
}

// MatchHostname returns an anonymized hostname and whether or not the hostname is in the whitelist.
func (c *Collector) MatchHostname(hostname string) (string, bool) {
	if c.enableHostnameWhitelist {
		hostSplit := strings.Split(hostname, ".")
		if hostSplit[0] == "www" {
			hostSplit = hostSplit[1:]
		}
		if match := c.hostnameWhitelist.getMatch(hostSplit); match != "" {
			if strings.HasPrefix(match, "*.") {
				hostSplit[0] = "*"
			}
			return strings.Join(hostSplit, "."), true
		}
		return "", false
	}

	return hostname, true
}

// GetCountryCode returns the country code for an IP address from the MaxMind GeoLite2 Country database.
func (c *Collector) GetCountryCode(ip net.IP) (string, error) {
	if c.geoIPDatabase == nil {
		return "", nil
	}

	geoIPRecord := getGeoIPCountryRecord()
	defer returnGeoIPCountryRecord(geoIPRecord)
	err := c.geoIPDatabase.Lookup(ip, &geoIPRecord)
	if err != nil {
		return "", err
	}
	return geoIPRecord.Country.IsoCode, nil
}
