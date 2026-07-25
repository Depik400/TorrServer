//go:build !ios

package torr

import (
	"net/http"

	"github.com/anacrolix/dms/dlna"
)

func setDLNAHeaders(w http.ResponseWriter) {
	w.Header().Set("transferMode.dlna.org", "Streaming")
}

func setDLNAContentFeatures(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("getContentFeatures.dlna.org") != "" {
		w.Header().Set("contentFeatures.dlna.org", dlna.ContentFeatures{
			SupportRange:    true,
			SupportTimeSeek: true,
		}.String())
	}
}
