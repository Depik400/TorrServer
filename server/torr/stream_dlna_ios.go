//go:build ios

package torr

import "net/http"

func setDLNAHeaders(w http.ResponseWriter) {}

func setDLNAContentFeatures(w http.ResponseWriter, r *http.Request) {}
