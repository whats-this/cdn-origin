package weed

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/valyala/fasthttp"
)

// lookupResponse represents a volume lookup response from the SeaweedFS master.
type lookupResponse struct {
	VolumeID  string `json:"volumeId"`
	Locations []struct {
		URL       string `json:"url"`
		PublicURL string `json:"publicUrl"`
	} `json:"locations"`
}

// publicUrlsToSlice converts the list of locations in the lookupResponse to a slice of publicURL strings.
func (res *lookupResponse) publicURLsToSlice() []string {
	var urls = []string{}
	for _, loc := range res.Locations {
		urls = append(urls, loc.PublicURL)
	}
	return urls
}

// Seaweed allows for retrieving files from a SeaweedFS cluster.
type Seaweed struct {
	// Connection URI for the master.
	Master string

	// Volume cache to use if CacheVolumes is true.
	VolumeCache *VolumeCache

	// Lookup timeout for fetching volumes from master.
	LookupTimeout time.Duration
}

// New creates a new instance of Seaweed.
func New(masterURI string, lookupTimeout time.Duration) *Seaweed {
	return &Seaweed{
		Master: masterURI,
		VolumeCache: &VolumeCache{
			volumeCache: map[uint32][]string{},
			next:        map[uint32]int{},
		},
		LookupTimeout: lookupTimeout,
	}
}

// Get a file from a SeaweedFS cluster.
func (s *Seaweed) Get(writer io.Writer, fid string, headers map[string][]byte, query string) (int, map[string][]byte, error) {
	volumeURL := s.lookupVolume(strings.Split(fid, ",")[0])
	if volumeURL == "" {
		return fasthttp.StatusInternalServerError, nil, errors.New("failed to retrieve volume URL")
	}
	requestURL := volumeURL
	if !strings.HasPrefix(requestURL, "http://") && !strings.HasPrefix(requestURL, "https://") {
		requestURL = "http://" + requestURL
	}
	if !strings.HasSuffix(requestURL, "/") {
		requestURL += "/"
	}
	requestURL += fid
	if len(query) != 0 {
		requestURL += "?" + query
	}

	// Set request and response
	req := fasthttp.AcquireRequest()
	req.Reset()
	req.SetRequestURI(requestURL)
	if headers != nil {
		for h, v := range headers {
			req.Header.SetBytesV(h, v)
		}
	}
	res := fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	// Perform request
	err := fasthttp.Do(req, res)
	if err != nil {
		return 0, nil, err
	}
	if res.StatusCode() == fasthttp.StatusOK || res.StatusCode() == fasthttp.StatusPartialContent {
		if err := res.BodyWriteTo(writer); err != nil {
			log.WithField("err", err).Warn("failed to set body writer for response")
			return fasthttp.StatusInternalServerError, nil, err
		}
	}

	// Get response headers
	resHeaders := map[string][]byte{}
	res.Header.VisitAll(func(key, value []byte) {
		resHeaders[string(key)] = value
	})
	return res.StatusCode(), resHeaders, err
}

// Ping the master of a SeaweedFS cluster (sends a /cluster/status HTTP request to the master).
func (s *Seaweed) Ping() error {
	lookupURL := fmt.Sprintf("%s/cluster/status", s.Master)
	statusCode, _, err := fasthttp.GetTimeout(nil, lookupURL, s.LookupTimeout)
	if err != nil {
		return err
	}
	if statusCode != fasthttp.StatusOK {
		return fmt.Errorf("expected 200 OK response status code, got %v", statusCode)
	}
	return nil
}

func (s *Seaweed) lookupVolume(volumeID string) string {
	volumeUint64, err := strconv.ParseUint(volumeID, 10, 32)
	if err != nil {
		log.WithFields(log.Fields{
			"err":      err,
			"volumeID": volumeID,
		}).Warn("could not parse volume ID")
		return ""
	}
	volumeUint32 := uint32(volumeUint64)
	if uri := s.VolumeCache.GetNext(volumeUint32); uri != "" {
		return uri
	}

	lookupURL := fmt.Sprintf("%s/dir/lookup?volumeId=%s", s.Master, volumeID)
	log.WithFields(log.Fields{
		"lookupURL": lookupURL,
		"volumeID":  volumeID,
	}).Debug("looking up volume from SeaweedFS master")
	statusCode, body, err := fasthttp.GetTimeout(nil, lookupURL, s.LookupTimeout)
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
			"url": lookupURL,
		}).Error("failed to lookup SeaweedFS volume from master")
		return ""
	}
	if statusCode != fasthttp.StatusOK {
		log.WithFields(log.Fields{
			"expected": fasthttp.StatusOK,
			"got":      statusCode,
		}).Warn("unexpected status code while looking up SeaweedFS volume from master")
		return ""
	}
	var res lookupResponse
	err = json.Unmarshal(body, &res)
	if err != nil {
		log.WithFields(log.Fields{
			"body": string(body),
			"err":  err,
		}).Error("failed to parse lookup volume response from SeaweedFS master")
		return ""
	}
	if len(res.Locations) == 0 {
		log.Warn("SeaweedFS master returned no volume servers without 404ing")
		return ""
	}
	s.VolumeCache.Add(volumeUint32, res.publicURLsToSlice())
	return s.VolumeCache.GetNext(volumeUint32)
}
