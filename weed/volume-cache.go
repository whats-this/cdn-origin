package weed

import "sync"

// VolumeCache stores a volume ID => volume URL map used for caching volume lookup responses from the master of a
// SeaweedFS cluster.
type VolumeCache struct {
	sync.RWMutex
	volumeCache map[uint32][]string
	next        map[uint32]int
}

// Add adds a volume ID => location URL slice mapping to the volume cache.
func (v *VolumeCache) Add(id uint32, urls []string) {
	v.Lock()
	defer v.Unlock()
	v.volumeCache[id] = urls
	v.next[id] = 0
}

// Get returns all volume server URLs for a given volume ID.
func (v *VolumeCache) Get(id uint32) []string {
	v.RLock()
	defer v.RUnlock()
	vol, _ := v.volumeCache[id]
	return vol
}

// GetNext returns the n+1th location URL for the given volume ID, n is tracked internally.
func (v *VolumeCache) GetNext(id uint32) string {
	v.RLock()
	defer v.RUnlock()
	vol, ok := v.volumeCache[id]
	volLen := len(vol)
	if !ok || volLen == 0 {
		return ""
	}
	n, _ := v.next[id] // It's okay if this fails, n will be 0 and v.next[id] will be set to 1
	if n >= volLen {
		n = 0
	}
	defer func() { v.next[id] = n + 1 }()
	return vol[n]
}

// Remove removes a volume from the volume cache.
func (v *VolumeCache) Remove(id uint32) {
	v.Lock()
	defer v.Unlock()
	delete(v.volumeCache, id)
}

// Empty clears all data and returns the VolumeCache to it's initial state.
func (v *VolumeCache) Empty() {
	v.Lock()
	defer v.Unlock()
	v.volumeCache = map[uint32][]string{}
	v.next = map[uint32]int{}
}
