module server

go 1.24.0

replace (
	github.com/anacrolix/torrent v1.59.1 => github.com/tsynik/torrent v1.2.22
	github.com/anacrolix/upnp v0.1.4 => github.com/tsynik/upnp v0.1.5
)

require (
	apptrix.org/components v0.0.0-20260710210331-fbae2353e5dc
	fyne.io/fyne/v2 v2.7.3
	github.com/agnivade/levenshtein v1.2.1
	github.com/alexflint/go-arg v1.6.0
	github.com/anacrolix/dms v1.7.2
	github.com/anacrolix/log v0.17.0
	github.com/anacrolix/missinggo/v2 v2.10.0
	github.com/anacrolix/publicip v0.3.1
	github.com/anacrolix/torrent v1.59.1
	github.com/dustin/go-humanize v1.0.1
	github.com/gin-contrib/cors v1.7.6
	github.com/gin-contrib/location/v2 v2.0.0
	github.com/gin-gonic/gin v1.11.0
	github.com/hanwen/go-fuse/v2 v2.9.0
	github.com/kljensen/snowball v0.10.0
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c
	github.com/pkg/errors v0.9.1
	github.com/swaggo/files v1.0.1
	github.com/swaggo/gin-swagger v1.6.1
	github.com/swaggo/swag v1.16.6
	github.com/wlynxg/anet v0.0.5
	go.etcd.io/bbolt v1.4.3
	golang.org/x/exp v0.0.0-20251125195548-87e1e737ad39
	golang.org/x/image v0.33.0
	golang.org/x/net v0.47.0
	golang.org/x/time v0.14.0
	gopkg.in/telebot.v4 v4.0.0-beta.7
	gopkg.in/vansante/go-ffprobe.v2 v2.2.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

require (
	fyne.io/systray v1.12.0 // indirect
	github.com/BurntSushi/toml v1.5.0 // indirect
	github.com/KyleBanks/depth v1.2.1 // indirect
	github.com/RoaringBitmap/roaring v1.9.4 // indirect
	github.com/alecthomas/atomic v0.1.0-alpha2 // indirect
	github.com/alexflint/go-scalar v1.2.0 // indirect
	github.com/anacrolix/chansync v0.7.0 // indirect
	github.com/anacrolix/dht/v2 v2.23.0 // indirect
	github.com/anacrolix/envpprof v1.4.0 // indirect
	github.com/anacrolix/ffprobe v1.1.0 // indirect
	github.com/anacrolix/generics v0.1.0 // indirect
	github.com/anacrolix/missinggo v1.3.0 // indirect
	github.com/anacrolix/missinggo/perf v1.0.0 // indirect
	github.com/anacrolix/multiless v0.4.0 // indirect
	github.com/anacrolix/stm v0.5.0 // indirect
	github.com/anacrolix/sync v0.5.4 // indirect
	github.com/anacrolix/upnp v0.1.4 // indirect
	github.com/anacrolix/utp v0.2.0 // indirect
	github.com/benbjohnson/immutable v0.4.3 // indirect
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/bradfitz/iter v0.0.0-20191230175014-e8f45d346db8 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.14.2 // indirect
	github.com/bytedance/sonic/loader v0.4.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/edsrzf/mmap-go v1.2.0 // indirect
	github.com/fredbi/uri v1.1.1 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fyne-io/gl-js v0.2.0 // indirect
	github.com/fyne-io/glfw-js v0.3.0 // indirect
	github.com/fyne-io/image v0.1.1 // indirect
	github.com/fyne-io/oksvg v0.2.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.11 // indirect
	github.com/gin-contrib/sse v1.1.0 // indirect
	github.com/go-gl/gl v0.0.0-20231021071112-07e5d0ea2e71 // indirect
	github.com/go-gl/glfw/v3.3/glfw v0.0.0-20240506104042-037f3cc74f2a // indirect
	github.com/go-openapi/jsonpointer v0.22.3 // indirect
	github.com/go-openapi/jsonreference v0.21.3 // indirect
	github.com/go-openapi/spec v0.22.1 // indirect
	github.com/go-openapi/swag/conv v0.25.4 // indirect
	github.com/go-openapi/swag/jsonname v0.25.4 // indirect
	github.com/go-openapi/swag/jsonutils v0.25.4 // indirect
	github.com/go-openapi/swag/loading v0.25.4 // indirect
	github.com/go-openapi/swag/stringutils v0.25.4 // indirect
	github.com/go-openapi/swag/typeutils v0.25.4 // indirect
	github.com/go-openapi/swag/yamlutils v0.25.4 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.28.0 // indirect
	github.com/go-text/render v0.2.0 // indirect
	github.com/go-text/typesetting v0.3.3 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/goccy/go-yaml v1.19.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/hack-pad/go-indexeddb v0.3.2 // indirect
	github.com/hack-pad/safejs v0.1.0 // indirect
	github.com/huandu/xstrings v1.5.0 // indirect
	github.com/jeandeaual/go-locale v0.0.0-20250612000132-0ef82f21eade // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jsummers/gobmp v0.0.0-20230614200233-a9de23ed2e25 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/nicksnyder/go-i18n/v2 v2.5.1 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.57.1 // indirect
	github.com/rs/dnscache v0.0.0-20230804202142-fc85eb664529 // indirect
	github.com/rymdport/portal v0.4.2 // indirect
	github.com/ryszard/goskiplist v0.0.0-20150312221310-2dfbae5fcf46 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.1 // indirect
	github.com/yuin/goldmark v1.7.8 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/arch v0.23.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/mod v0.30.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/tools v0.39.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
