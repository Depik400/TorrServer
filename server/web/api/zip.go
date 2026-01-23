package api

import (
	"archive/zip"
	"io"
	"net/http"
	"server/log"
	"server/torr"
	"server/web/api/utils"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// downloadZip godoc
//
//	@Summary		Download all torrent files as ZIP archive
//	@Description	Download all files from torrent as ZIP archive with folder structure preserved.
//
//	@Tags			API
//
//	@Param			hash	query	string	true	"Torrent hash"
//
//	@Produce		application/zip
//	@Success		200	"ZIP archive with all torrent files"
//	@Router			/downloadzip [get]
func downloadZip(c *gin.Context) {
	hash := c.Query("hash")
	if hash == "" {
		c.AbortWithError(http.StatusBadRequest, errors.New("hash is required"))
		return
	}

	spec, err := utils.ParseLink(hash)
	if err != nil {
		log.TLogln("error parse link:", err)
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	tor := torr.GetTorrent(spec.InfoHash.HexString())
	if tor == nil {
		c.AbortWithError(http.StatusNotFound, errors.New("torrent not found"))
		return
	}

	if !tor.GotInfo() {
		c.AbortWithError(http.StatusInternalServerError, errors.New("torrent connection timeout"))
		return
	}

	st := tor.Status()
	if len(st.FileStats) == 0 {
		c.AbortWithError(http.StatusBadRequest, errors.New("no files in torrent"))
		return
	}

	// Set headers for ZIP download
	zipName := st.Title
	if zipName == "" && st.Name != "" {
		zipName = st.Name
	}
	if zipName == "" && tor.Torrent != nil && tor.Torrent.Info() != nil {
		zipName = tor.Torrent.Info().Name
	}
	if zipName == "" {
		zipName = hash
	}
	// Clean filename for use in Content-Disposition header
	zipName = strings.ReplaceAll(zipName, "/", "_")
	zipName = strings.ReplaceAll(zipName, "\\", "_")
	zipName = strings.ReplaceAll(zipName, ":", "_")
	zipName = strings.ReplaceAll(zipName, "*", "_")
	zipName = strings.ReplaceAll(zipName, "?", "_")
	zipName = strings.ReplaceAll(zipName, "\"", "_")
	zipName = strings.ReplaceAll(zipName, "<", "_")
	zipName = strings.ReplaceAll(zipName, ">", "_")
	zipName = strings.ReplaceAll(zipName, "|", "_")
	if !strings.HasSuffix(strings.ToLower(zipName), ".zip") {
		zipName += ".zip"
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", `attachment; filename="`+zipName+`"`)
	c.Header("Content-Transfer-Encoding", "binary")

	// Create ZIP writer
	zipWriter := zip.NewWriter(c.Writer)
	defer zipWriter.Close()

	// Add all files to ZIP archive
	for _, fileStat := range st.FileStats {
		// Find the actual torrent file
		files := tor.Files()
		var file *torrent.File
		for _, tfile := range files {
			if tfile.Path() == fileStat.Path {
				file = tfile
				break
			}
		}
		if file == nil {
			log.TLogln("file not found:", fileStat.Path)
			continue
		}

		// Create reader for file
		reader := tor.NewReader(file)
		if reader == nil {
			log.TLogln("cannot create reader for file:", fileStat.Path)
			continue
		}

		// Create file entry in ZIP with full path
		zipPath := fileStat.Path
		// Normalize path separators for ZIP
		zipPath = strings.ReplaceAll(zipPath, "\\", "/")
		// Remove leading slash if present
		if strings.HasPrefix(zipPath, "/") {
			zipPath = zipPath[1:]
		}

		zipFile, err := zipWriter.Create(zipPath)
		if err != nil {
			log.TLogln("error creating zip entry:", err)
			tor.CloseReader(reader)
			continue
		}

		// Copy file content to ZIP
		_, err = io.Copy(zipFile, reader)
		if err != nil {
			log.TLogln("error copying file to zip:", err)
		}

		tor.CloseReader(reader)
	}

	// Flush any remaining data
	zipWriter.Flush()
}
