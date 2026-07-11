package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"server/torrfs/fuse"
	"server/torrfs/webdav"

	"server/rutor"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/location/v2"
	"github.com/gin-gonic/gin"
	"github.com/wlynxg/anet"

	"server/dlna"
	"server/settings"
	"server/web/msx"

	"server/log"
	"server/torr"
	"server/version"
	"server/web/api"
	"server/web/auth"
	"server/web/blocker"
	"server/web/pages"
	"server/web/sslcerts"

	swaggerFiles "github.com/swaggo/files"     // swagger embed files
	ginSwagger "github.com/swaggo/gin-swagger" // gin-swagger middleware
)

var (
	BTS         = torr.NewBTS()
	waitChan    = make(chan error, 2)
	httpServer  *http.Server
	httpsServer *http.Server
	stopOnce    sync.Once
)

//	@title			Swagger Torrserver API
//	@version		{version.Version}
//	@description	Torrent streaming server.

//	@license.name	GPL 3.0

//	@BasePath	/

//	@securityDefinitions.basic	BasicAuth

// @externalDocs.description	OpenAPI
// @externalDocs.url			https://swagger.io/resources/open-api/
func Start(httpListener net.Listener) error {
	waitChan = make(chan error, 2)
	httpServer = nil
	httpsServer = nil
	stopOnce = sync.Once{}

	log.TLogln("Start TorrServer " + version.Version + " torrent " + version.GetTorrentVersion())
	ips := GetLocalIps()
	if len(ips) > 0 {
		log.TLogln("Local IPs:", ips)
	}
	err := BTS.Connect()
	if err != nil {
		log.TLogln("BTS.Connect() error!", err)
		return err
	}
	rutor.Start()

	gin.SetMode(gin.ReleaseMode)

	// corsCfg := cors.DefaultConfig()
	// corsCfg.AllowAllOrigins = true
	// corsCfg.AllowHeaders = []string{"*"}
	// corsCfg.AllowMethods = []string{"*"}
	corsCfg := cors.Config{
		AllowOrigins: []string{"http://localhost:5173"}, // Точнее указать origin
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Content-Length",
			"Accept-Encoding",
			"X-CSRF-Token",
			"Authorization",
			"Accept",
			"X-Requested-With",
			"X-Api-Key",
		},
		ExposeHeaders:    []string{"Content-Length", "Content-Type"},
		AllowCredentials: true, // Если используете куки/авторизацию
		MaxAge:           12 * time.Hour,
	}
	route := gin.New()
	route.Use(log.WebLogger(), cors.New(corsCfg), blocker.Blocker(), gin.Recovery(), location.Default())
	auth.SetupAuth(route)

	route.GET("/api/echo", echo)

	api.SetupRoute(route)
	msx.SetupRoute(route)
	pages.SetupRoute(route)
	if settings.Args.WebDAV {
		webdav.MountWebDAV(route)
	}

	if settings.BTsets.EnableDLNA {
		dlna.Start()
	}

	// Auto-mount FUSE filesystem if enabled
	fuse.FuseAutoMount()

	route.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	if settings.Ssl {
		if settings.BTsets.SslCert == "" || settings.BTsets.SslKey == "" {
			settings.BTsets.SslCert, settings.BTsets.SslKey = sslcerts.MakeCertKeyFiles(ips)
			log.TLogln("Saving path to ssl cert and key in db", settings.BTsets.SslCert, settings.BTsets.SslKey)
			settings.SetBTSets(settings.BTsets)
		}
		err = sslcerts.VerifyCertKeyFiles(settings.BTsets.SslCert, settings.BTsets.SslKey, settings.SslPort)
		if err != nil {
			log.TLogln("Error checking certificate and private key files:", err)
			settings.BTsets.SslCert, settings.BTsets.SslKey = sslcerts.MakeCertKeyFiles(ips)
			log.TLogln("Saving path to ssl cert and key in db", settings.BTsets.SslCert, settings.BTsets.SslKey)
			settings.SetBTSets(settings.BTsets)
		}

		httpsListener, listenErr := net.Listen("tcp", net.JoinHostPort(settings.IP, settings.SslPort))
		if listenErr != nil {
			return listenErr
		}
		if tcpAddr, ok := httpsListener.Addr().(*net.TCPAddr); ok {
			settings.SslPort = strconv.Itoa(tcpAddr.Port)
		}

		httpsServer = &http.Server{Handler: route}
		go func() {
			log.TLogln("Start https server at", httpsListener.Addr().String())
			waitChan <- normalizeServeErr(httpsServer.ServeTLS(httpsListener, settings.BTsets.SslCert, settings.BTsets.SslKey))
		}()
	}

	if httpListener == nil {
		httpListener, err = net.Listen("tcp", net.JoinHostPort(settings.IP, settings.Port))
		if err != nil {
			return err
		}
	}
	if tcpAddr, ok := httpListener.Addr().(*net.TCPAddr); ok {
		settings.Port = strconv.Itoa(tcpAddr.Port)
	}

	httpServer = &http.Server{Handler: route}
	go func() {
		log.TLogln("Start http server at", httpListener.Addr().String())
		waitChan <- normalizeServeErr(httpServer.Serve(httpListener))
	}()

	return nil
}

func Wait() error {
	return <-waitChan
}

func Stop() {
	stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if httpServer != nil {
			_ = httpServer.Shutdown(ctx)
		}
		if httpsServer != nil {
			_ = httpsServer.Shutdown(ctx)
		}

		dlna.Stop()
		fuse.FuseCleanup()
		BTS.Disconnect()
	})
}

func normalizeServeErr(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// echo godoc
//
//	@Summary		Tests server status
//	@Description	Tests whether server is alive or not
//
//	@Tags			API
//
//	@Produce		plain
//	@Success		200	{string}	string	"Server version"
//	@Router			/echo [get]
func echo(c *gin.Context) {
	c.String(200, "%v", version.Version)
}

func GetLocalIps() []string {
	ifaces, err := anet.Interfaces()
	if err != nil {
		log.TLogln("Error get local IPs")
		return nil
	}
	var list []string
	for _, i := range ifaces {
		addrs, _ := anet.InterfaceAddrsByInterface(&i)
		if i.Flags&net.FlagUp == net.FlagUp {
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() {
					list = append(list, ip.String())
				}
			}
		}
	}
	sort.Strings(list)
	return list
}
