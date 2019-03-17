package thumbnailer

import (
	"io"
	"os"
	"path/filepath"
)

// ThumbnailCache allows access to thumbnails stored in a directory. Each
// thumbnail has a key, which uniquely identifies it. The key should be a unique
// ID from a database or the original file's hash.
type ThumbnailCache struct {
	Directory string
}

// NewThumbnailCache creates a new *ThumbnailCache.
func NewThumbnailCache(directory string) *ThumbnailCache {
	return &ThumbnailCache{
		Directory: directory,
	}
}

// GetThumbnail returns a thumbnail that is cached. If no cached copy exists, a
// exists, a NoCachedCopy error is returned.
func (c *ThumbnailCache) GetThumbnail(key string) (io.ReadCloser, error) {
	path := filepath.Join(c.Directory, key)
	data, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, NoCachedCopy
	}
	return data, err
}

// SetThumbnail stores a thumbnail with the specified key.
func (c *ThumbnailCache) SetThumbnail(key string, data io.Reader) error {
	path := filepath.Join(c.Directory, key)
	file, err := os.Create(path)
	defer file.Close()
	if err != nil {
		return err
	}
	_, err = io.Copy(file, data)
	return err
}

// Transform generates a thumbnail and caches it.
func (c *ThumbnailCache) Transform(key string, data io.Reader) error {
	outputImage, err := Transform(data)
	if err != nil {
		return err
	}
	return c.SetThumbnail(key, outputImage)
}

// DeleteThumbnail deletes a thumbnail from the cache.
func (c *ThumbnailCache) DeleteThumbnail(key string) error {
	path := filepath.Join(c.Directory, key)
	return os.Remove(path)
}
