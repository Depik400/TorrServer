//go:build !ios
// +build !ios

package torr

import (
	"strconv"

	"server/ffprobe"
	"server/settings"
)

func (t *Torrent) probeMedia(index int) {
	if !ffprobe.Exists() {
		return
	}
	link := "http://127.0.0.1:" + settings.Port + "/play/" + t.Hash().HexString() + "/" + strconv.Itoa(index)
	if settings.Ssl {
		link = "https://127.0.0.1:" + settings.SslPort + "/play/" + t.Hash().HexString() + "/" + strconv.Itoa(index)
	}
	if data, err := ffprobe.ProbeUrl(link); err == nil {
		t.BitRate = data.Format.BitRate
		t.DurationSeconds = data.Format.DurationSeconds
	}
}
