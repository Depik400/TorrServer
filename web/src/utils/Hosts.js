const { protocol, hostname, port } = window.location

let torrserverHost = process.env.REACT_APP_SERVER_HOST || `${protocol}//${hostname}${port ? `:${port}` : ''}`

export const torrentsHost = () => `${torrserverHost}/api/torrents`
export const viewedHost = () => `${torrserverHost}/api/viewed`
export const cacheHost = () => `${torrserverHost}/api/cache`
export const torrentUploadHost = () => `${torrserverHost}/api/torrent/upload`
export const settingsHost = () => `${torrserverHost}/api/settings`
export const streamHost = () => `${torrserverHost}/api/stream`
export const shutdownHost = () => `${torrserverHost}/api/shutdown`
export const echoHost = () => `${torrserverHost}/api/echo`
export const playlistTorrHost = () => `${torrserverHost}/api/stream`
export const torznabSearchHost = () => `${torrserverHost}/api/torznab/search`
export const searchHost = () => `${torrserverHost}/api/search`
export const torznabTestHost = () => `${torrserverHost}/api/torznab/test`
export const downloadZipHost = () => `${torrserverHost}/api/downloadzip`

export const getTorrServerHost = () => torrserverHost
export const setTorrServerHost = host => {
  torrserverHost = host
}
