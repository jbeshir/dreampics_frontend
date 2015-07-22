package storage

import (
	"config"
)

var (
	gcsBucket = config.Get("GCS_BUCKET")
)

func init() {
	if gcsBucket == "" {
		panic("Missing GCS_BUCKET environmental variable.")
	}
}
