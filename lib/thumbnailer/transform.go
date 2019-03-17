package thumbnailer

import (
	"bytes"
	"io"
	"io/ioutil"
	"sync"

	"github.com/discordapp/lilliput"
	"github.com/pkg/errors"
)

// Transform settings. These should remain constant. If these are ever changed,
// the thumbnail cache should be cleared.
var (
	maxInputResolution  = 10000           // maximum length in pixels of any input length
	maxOutputResolution = 200             // maximum length in pixels of any output length
	outputBufferSize    = 5 * 1024 * 1024 // bytes
	outputType          = ".jpeg"         // with leading dot
	encodeOptions       = map[int]int{
		lilliput.JpegQuality: 85,
	}
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
	_, ok := thumbnailMIMETypes[mime]
	return ok
}

// Pool operations for reusing *lillput.ImageOps objects.
var imageOpsPool = &sync.Pool{
	New: func() interface{} {
		return lilliput.NewImageOps(maxInputResolution)
	},
}

func getImageOps() *lilliput.ImageOps {
	return imageOpsPool.Get().(*lilliput.ImageOps)
}
func returnImageOps(imageOps *lilliput.ImageOps) {
	imageOpsPool.Put(imageOps)
}

// Transform takes image data and resizes it to fit the maxOutputResolution above.
func Transform(data io.Reader) (*bytes.Buffer, error) {
	b, err := ioutil.ReadAll(data)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read in image data")
	}

	// Create decoder and parse header
	decoder, err := lilliput.NewDecoder(b)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode image data")
	}
	header, err := decoder.Header()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse image header")
	}

	// Check if the image is within the input limit
	if header.Width() > maxInputResolution || header.Height() > maxInputResolution {
		return nil, InputTooLarge
	}

	// Determine output resolution
	width := -1
	height := -1
	if header.Width() == header.Height() {
		width = maxOutputResolution
		height = maxOutputResolution
	} else if header.Width() > header.Height() {
		width = 200
		scale := header.Width() / maxOutputResolution
		height = header.Height() / scale
	} else {
		height = 200
		scale := header.Height() / maxOutputResolution
		width = header.Width() / scale
	}

	// Prepare to resize image
	ops := getImageOps()
	outputImage := make([]byte, outputBufferSize)

	// Resizing options
	opts := &lilliput.ImageOptions{
		FileType:             outputType,
		Width:                width,
		Height:               height,
		ResizeMethod:         lilliput.ImageOpsFit,
		NormalizeOrientation: true,
		EncodeOptions:        encodeOptions,
	}
	if header.Width() <= maxOutputResolution && header.Height() <= maxOutputResolution {
		opts.Width = header.Width()
		opts.Height = header.Height()
		opts.ResizeMethod = lilliput.ImageOpsNoResize
	}

	// Transform the image
	outputImage, err = ops.Transform(decoder, opts, outputImage)
	if err != nil {
		return nil, errors.Wrap(err, "failed to transcode image")
	}
	returnImageOps(ops)
	return bytes.NewBuffer(outputImage), err
}
