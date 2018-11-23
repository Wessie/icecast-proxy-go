package server

import (
	"log"
	iconv "github.com/Wessie/go-iconv"
)

func ParseMetadata(charset string, meta string) (metadata string) {
	if charset == "latin1" {
		log.Printf("latin1")
		if res, err := iconv.Conv(meta, "UTF-8", "UTF-8"); err == nil {
			metadata = res
		} else if res, err := iconv.Conv(meta, "UTF-8", "SHIFT_JIS"); err == nil {
			metadata = res
		} else {
			//metadata, _ = iconv.Conv(meta, "UTF8//TRANSLIT", "UTF8")
		}
	} else {
		metadata = meta
	}

	return metadata
}
