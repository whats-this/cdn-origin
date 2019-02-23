package metrics

import "sync"

var geoIPPool = &sync.Pool{
	New: func() interface{} {
		return &geoIPCountryRecord{}
	},
}

func getGeoIPCountryRecord() *geoIPCountryRecord {
	return geoIPPool.Get().(*geoIPCountryRecord)
}

func returnGeoIPCountryRecord(record *geoIPCountryRecord) {
	go func() {
		record.Country.IsoCode = ""
		geoIPPool.Put(record)
	}()
}

type geoIPCountryRecord struct {
	Country struct {
		IsoCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
}
