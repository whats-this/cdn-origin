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
	VolumeId  string `json:"volumeId"`
	Locations []struct {
		Url       string `json:"url"`
		PublicURL string `json:"publicUrl"`
	} `json:"locations"`
}

// publicUrlsToSlice converts the list of locations in the lookupResponse to a slice of publicUrl strings.
func (res *lookupResponse) publicUrlsToSlice() []string {
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

func (s *Seaweed) Get(writer io.Writer, fid string, query string) (int, error) {
	volumeURL := s.lookupVolume(strings.Split(fid, ",")[0])
	if volumeURL == "" {
		return 500, errors.New("failed to retrieve volume URL")
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
	res := fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	// Perform request
	err := fasthttp.Do(req, res)
	if err != nil {
		return 0, err
	}
	if res.StatusCode() == fasthttp.StatusOK {
		if err := res.BodyWriteTo(writer); err != nil {
			log.WithField("err", err).Warn("failed to set body writer for response")
			return fasthttp.StatusInternalServerError, err
		}
	}
	return fasthttp.StatusOK, err
}

func (s *Seaweed) Ping() error {
	lookupURL := fmt.Sprintf("%s/cluster/status", s.Master)
	statusCode, _, err := fasthttp.GetTimeout(nil, lookupURL, s.LookupTimeout)
	if err != nil {
		return err
	}
	if statusCode != fasthttp.StatusOK {
		return errors.New(fmt.Sprintf("expected 200 OK response status code, got %v", statusCode))
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
	s.VolumeCache.Add(volumeUint32, res.publicUrlsToSlice())
	return s.VolumeCache.GetNext(volumeUint32)
}
