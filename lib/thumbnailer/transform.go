package thumbnailer

import (
	"bytes"
	"io"
	"strings"

	"github.com/pkg/errors"
	"github.com/valyala/fasthttp"
)

// Accepted MIME types for thumbnails in map for easy checking
var thumbnailMIMETypes = map[string]struct{}{
	"image/gif":  struct{}{},
	"image/jpeg": struct{}{},
	"image/png":  struct{}{},
	"image/webp": struct{}{},
}

// AcceptedMIMEType checks if a MIME type is suitable for thumbnailing.
func AcceptedMIMEType(mime string) bool {
	mimes := strings.SplitN(mime, ";", 2)
	_, ok := thumbnailMIMETypes[mimes[0]]
	return ok
}

// Transform takes an image io.Reader and sends it to the thumbnailer service
// to be transcoded into a thumbnail.
func Transform(thumbnailerURL, contentType string, data io.Reader) (*bytes.Buffer, error) {
	// Set request and response
	req := fasthttp.AcquireRequest()
	res := fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(res)
	}()

	req.Reset()
	req.Header.SetMethod("POST")
	req.SetRequestURI(thumbnailerURL)
	req.Header.Set("Content-Type", contentType)
	_, err := io.Copy(req.BodyWriter(), data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to copy data to request")
	}
	res.Reset()

	// Do request
	err = fasthttp.Do(req, res)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make request to thumbnailer service")
	}
	if res.StatusCode() != fasthttp.StatusOK {
		return nil, errors.Errorf("thumbnailer service failed to create thumbnail: %s", string(res.Body()))
	}

	return bytes.NewBuffer(res.Body()), nil
}
