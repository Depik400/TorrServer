package api

import (
	config "server/settings"
	"server/web/auth"

	"github.com/gin-gonic/gin"
)

type requestI struct {
	Action string `json:"action,omitempty"`
}

func SetupRoute(route gin.IRouter) {
	api := route.Group("/api")
	authorized := api.Group("/", auth.CheckAuth())

	authorized.GET("/shutdown", shutdown)
	authorized.GET("/shutdown/*reason", shutdown)

	authorized.POST("/settings", settings)
	authorized.POST("/torznab/test", torznabTest)

	authorized.POST("/torrents", torrents)

	authorized.POST("/torrent/upload", torrentUpload)

	authorized.POST("/cache", cache)

	api.HEAD("/stream", stream)
	api.GET("/stream", stream)

	api.HEAD("/stream/*fname", stream)
	api.GET("/stream/*fname", stream)

	api.HEAD("/play/:hash/:id", play)
	api.GET("/play/:hash/:id", play)

	authorized.POST("/viewed", viewed)

	authorized.GET("/playlistall/all.m3u", allPlayList)

	api.GET("/playlist", playList)
	api.GET("/playlist/*fname", playList)

	authorized.GET("/download/:size", download)

	authorized.GET("/downloadzip", downloadZip)

	if config.SearchWA {
		api.GET("/search/*query", rutorSearch)
	} else {
		authorized.GET("/search/*query", rutorSearch)
	}

	if config.SearchWA {
		api.GET("/torznab/search/*query", torznabSearch)
	} else {
		authorized.GET("/torznab/search/*query", torznabSearch)
	}

	// Add storage settings endpoints
	authorized.GET("/storage/settings", GetStorageSettings)
	authorized.POST("/storage/settings", UpdateStorageSettings)

	authorized.GET("/ffp/:hash/:id", ffp)
}
