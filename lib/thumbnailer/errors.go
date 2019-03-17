package thumbnailer

type thumbnailerError struct {
	Err string
}

// Error implements error.
func (e *thumbnailerError) Error() string {
	return e.Err
}

// NoCachedCopy means there is no cached copy for the specified key available.
var NoCachedCopy error = &thumbnailerError{"no cached copy of the thumbnail requested is available"}

// InputTooLarge means that the pixel size of the input image is too big to be thumbnailed.
var InputTooLarge error = &thumbnailerError{"the input size in pixels is too large"}
