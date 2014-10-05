package server

import (
	iconv "github.com/sloonz/go-iconv"
)

func ParseMetadata(charset string, meta string) (metadata string) {
	if charset == "latin1" {
		if res, err := iconv.Conv(meta, "UTF-8", "UTF-8"); err == nil {
			metadata = res
		} else if res, err := iconv.Conv(meta, "UTF-8", "SHIFT_JIS"); err == nil {
			metadata = res
		} else {
			metadata, _ = iconv.Conv(meta, "UTF8//TRANSLIT", "UTF8")
		}
	} else {
		// We trust outside sources? burn the books!
		metadata, _ = iconv.Conv(meta, "UTF-8//TRANSLIT", charset)
	}
	return metadata
}
