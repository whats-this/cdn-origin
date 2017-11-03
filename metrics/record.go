package metrics

import "sync"

var recordPool = &sync.Pool{
	New: func() interface{} {
		return &Record{}
	},
}

// GetRecord returns a `*Record` with all properties set to their zero values.
func GetRecord() *Record {
	return recordPool.Get().(*Record)
}

// ReturnRecord returns a record to the `Record` pool. Once a record has been returned, its properties must not be
// altered.
func ReturnRecord(record *Record) {
	go func() {
		record.CountryCode = ""
		record.Hostname = ""
		record.ObjectType = ""
		record.StatusCode = 0
		recordPool.Put(record)
	}()
}

// Record represents request metadata to be stored in Elasticsearch. When using `Record`s, it is recommended to use the
// pool methods `GetRecord()` and `ReturnRecord(*Record)` to reduce garbage colllector load and improve performance.
type Record struct {
	CountryCode string `json:"country_code,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	ObjectType  string `json:"object_type,omitempty"`
	StatusCode  int    `json:"status_code"`
}
