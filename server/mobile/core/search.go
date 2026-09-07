package core

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"server/log"
	"server/rutor"
	"server/rutor/torrsearch"
	sets "server/settings"
	"server/torznab"
)

// providerSearchTimeout bounds each provider (one torznab indexer, or rutor).
// One slow/broken indexer must not hold up the aggregated response.
const providerSearchTimeout = 12 * time.Second

// IndexerConfig is a single torznab/Jackett/Prowlarr endpoint. In the MVP the
// client owns the indexer list (it is not persisted in the core DB) and passes
// it in every TS_Search call.
type IndexerConfig struct {
	Name       string   `json:"name"`
	TorznabURL string   `json:"torznabURL"`
	APIKey     string   `json:"apiKey"`
	Categories []string `json:"categories,omitempty"`
}

// SearchOpts carries everything Search needs beyond the free-text query.
type SearchOpts struct {
	// Category is one of: all / video / movies / tv / anime / music / other.
	// Empty is treated as "all".
	Category     string          `json:"category,omitempty"`
	Indexers     []IndexerConfig `json:"indexers,omitempty"`
	RutorEnabled bool            `json:"rutorEnabled,omitempty"`
}

// SearchResult is one aggregated, de-duplicated hit.
type SearchResult struct {
	Title       string `json:"title"`
	Magnet      string `json:"magnet,omitempty"`
	TorrentURL  string `json:"torrentUrl,omitempty"`
	Infohash    string `json:"infohash,omitempty"`
	SizeBytes   int64  `json:"sizeBytes"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	Tracker     string `json:"tracker"`
	PublishedAt int64  `json:"publishedAt,omitempty"` // unix seconds
	Category    string `json:"category"`
	PosterURL   string `json:"posterUrl,omitempty"`
}

// ProviderError notes a single provider that failed or returned nothing usable;
// the rest of the aggregated result is still returned.
type ProviderError struct {
	Provider string `json:"provider"`
	Message  string `json:"message"`
}

// IndexerTestResult is the outcome of a torznab t=caps probe.
type IndexerTestResult struct {
	OK         bool     `json:"ok"`
	Message    string   `json:"message"`
	Categories []string `json:"categories,omitempty"`
}

// Search fans out to every enabled provider in parallel, aggregates and
// de-duplicates by infohash / magnet / torrent URL, and sorts by
// (seeders desc, publishedAt desc). A provider failing only adds a
// ProviderError; it never fails the whole call.
func (e *Engine) Search(query string, opts SearchOpts) ([]SearchResult, []ProviderError, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil, newEngineError(ErrInvalidArgument, "empty query")
	}

	category := strings.ToLower(strings.TrimSpace(opts.Category))
	if category == "" {
		category = "all"
	}

	type provider struct {
		name string
		run  func(ctx context.Context) ([]SearchResult, error)
	}
	var providers []provider

	for _, idx := range opts.Indexers {
		idx := idx
		name := idx.Name
		if name == "" {
			name = idx.TorznabURL
		}
		if strings.TrimSpace(idx.TorznabURL) == "" {
			continue
		}
		providers = append(providers, provider{
			name: name,
			run: func(ctx context.Context) ([]SearchResult, error) {
				return searchTorznab(ctx, idx, query, category)
			},
		})
	}

	if opts.RutorEnabled {
		e.ensureRutorStarted()
		providers = append(providers, provider{
			name: "rutor",
			run: func(ctx context.Context) ([]SearchResult, error) {
				return searchRutor(query)
			},
		})
	}

	if len(providers) == 0 {
		return []SearchResult{}, []ProviderError{}, nil
	}

	var (
		mu       sync.Mutex
		all      []SearchResult
		provErrs []ProviderError
		wg       sync.WaitGroup
	)

	for _, p := range providers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					provErrs = append(provErrs, ProviderError{Provider: p.name, Message: fmt.Sprintf("panic: %v", r)})
					mu.Unlock()
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), providerSearchTimeout)
			defer cancel()

			res, err := p.run(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				provErrs = append(provErrs, ProviderError{Provider: p.name, Message: err.Error()})
				return
			}
			all = append(all, res...)
		}()
	}
	wg.Wait()

	results := dedupResults(all)
	if category != "all" {
		filtered := results[:0]
		for _, r := range results {
			if matchesCategory(r.Category, category) {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Seeders != results[j].Seeders {
			return results[i].Seeders > results[j].Seeders
		}
		return results[i].PublishedAt > results[j].PublishedAt
	})

	if results == nil {
		results = []SearchResult{}
	}
	if provErrs == nil {
		provErrs = []ProviderError{}
	}
	return results, provErrs, nil
}

// TestIndexer probes a torznab endpoint with t=caps and returns the advertised
// top-level category names on success.
func (e *Engine) TestIndexer(rawURL, apiKey string) IndexerTestResult {
	ctx, cancel := context.WithTimeout(context.Background(), providerSearchTimeout)
	defer cancel()

	endpoint, err := torznabEndpoint(rawURL)
	if err != nil {
		return IndexerTestResult{OK: false, Message: err.Error()}
	}
	q := endpoint.Query()
	q.Set("t", "caps")
	if apiKey != "" {
		q.Set("apikey", apiKey)
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return IndexerTestResult{OK: false, Message: err.Error()}
	}
	resp, err := searchHTTPClient(providerSearchTimeout).Do(req)
	if err != nil {
		return IndexerTestResult{OK: false, Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return IndexerTestResult{OK: false, Message: fmt.Sprintf("HTTP %s", resp.Status)}
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var probe struct {
		XMLName    xml.Name
		Code       string `xml:"code,attr"`
		Desc       string `xml:"description,attr"`
		Categories struct {
			Category []struct {
				Name string `xml:"name,attr"`
			} `xml:"category"`
		} `xml:"categories"`
	}
	if err := xml.Unmarshal(body, &probe); err != nil {
		return IndexerTestResult{OK: false, Message: "invalid XML response"}
	}
	if probe.XMLName.Local == "error" {
		msg := probe.Desc
		if msg == "" {
			msg = probe.Code
		}
		return IndexerTestResult{OK: false, Message: "indexer error: " + msg}
	}
	if probe.XMLName.Local != "caps" {
		return IndexerTestResult{OK: false, Message: "unexpected response root: " + probe.XMLName.Local}
	}

	var cats []string
	for _, c := range probe.Categories.Category {
		if c.Name != "" {
			cats = append(cats, c.Name)
		}
	}
	return IndexerTestResult{OK: true, Message: "OK", Categories: cats}
}

// SearchProviderStatus is a lightweight readiness report for the settings UI.
type SearchProviderStatus struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // "torznab" | "rutor"
	Ready   bool   `json:"ready"`
	Message string `json:"message,omitempty"`
}

// SearchProviders reports whether each provider the client would use is usable
// right now. Torznab endpoints are "ready" once a URL is present (a real probe
// is TS_TestIndexer); rutor is ready once its DB has finished indexing.
func (e *Engine) SearchProviders(indexers []IndexerConfig, rutorEnabled bool) []SearchProviderStatus {
	out := make([]SearchProviderStatus, 0, len(indexers)+1)
	for _, idx := range indexers {
		name := idx.Name
		if name == "" {
			name = idx.TorznabURL
		}
		st := SearchProviderStatus{Name: name, Kind: "torznab"}
		if strings.TrimSpace(idx.TorznabURL) == "" {
			st.Message = "нет URL"
		} else {
			st.Ready = true
		}
		out = append(out, st)
	}
	if rutorEnabled {
		st := SearchProviderStatus{Name: "rutor", Kind: "rutor"}
		e.mu.Lock()
		started := e.rutorStarted
		e.mu.Unlock()
		switch {
		case !started:
			st.Message = "не запущен"
		case len(torrsearch.GetIDX()) == 0:
			st.Message = "база загружается"
		default:
			st.Ready = true
		}
		out = append(out, st)
	}
	return out
}

// ensureRutorStarted flips the settings flag and kicks off the rutor DB
// download/index exactly once. The DB is large, so the first search after
// enabling rutor usually returns a "still loading" provider error.
func (e *Engine) ensureRutorStarted() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if sets.BTsets != nil {
		sets.BTsets.EnableRutorSearch = true
	}
	if e.rutorStarted {
		return
	}
	e.rutorStarted = true
	rutor.Start()
}

func searchRutor(query string) ([]SearchResult, error) {
	list := rutor.Search(query)
	if len(list) == 0 {
		if len(torrsearch.GetIDX()) == 0 {
			return nil, errors.New("база rutor ещё загружается, повторите позже")
		}
		return nil, nil
	}

	out := make([]SearchResult, 0, len(list))
	for _, d := range list {
		if d == nil {
			continue
		}
		magnet := d.Magnet
		infohash := strings.ToLower(strings.TrimSpace(d.Hash))
		if magnet == "" && infohash != "" {
			magnet = "magnet:?xt=urn:btih:" + infohash
		}
		if infohash == "" {
			infohash = infohashFromMagnet(magnet)
		}
		var pub int64
		if !d.CreateDate.IsZero() {
			pub = d.CreateDate.Unix()
		}
		tracker := d.Tracker
		if tracker == "" {
			tracker = "rutor"
		}
		torrentURL := ""
		if strings.HasPrefix(d.Link, "http") {
			torrentURL = d.Link
		}
		out = append(out, SearchResult{
			Title:       firstNonEmpty(d.Title, d.Name),
			Magnet:      magnet,
			TorrentURL:  torrentURL,
			Infohash:    infohash,
			SizeBytes:   parseHumanSize(d.Size),
			Seeders:     d.Seed,
			Leechers:    d.Peer,
			Tracker:     tracker,
			PublishedAt: pub,
			Category:    categoryFromRutor(d.Categories),
		})
	}
	return out, nil
}

func searchTorznab(ctx context.Context, idx IndexerConfig, query, category string) ([]SearchResult, error) {
	endpoint, err := torznabEndpoint(idx.TorznabURL)
	if err != nil {
		return nil, err
	}

	q := endpoint.Query()
	q.Set("t", "search")
	q.Set("q", query)
	if idx.APIKey != "" {
		q.Set("apikey", idx.APIKey)
	}
	if cats := torznabCatsFor(category, idx.Categories); cats != "" {
		q.Set("cat", cats)
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := searchHTTPClient(providerSearchTimeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	var parsed torznab.TorznabResponse
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("bad XML: %v", err)
	}

	tracker := idx.Name
	if tracker == "" {
		tracker = endpoint.Host
	}

	out := make([]SearchResult, 0, len(parsed.Channel.Items))
	for _, item := range parsed.Channel.Items {
		r := SearchResult{
			Title:       item.Title,
			Tracker:     tracker,
			PublishedAt: parseRSSDate(item.PubDate),
			Category:    "other",
		}

		if item.Size > 0 {
			r.SizeBytes = item.Size
		}
		for _, enc := range item.Enclosure {
			if strings.HasPrefix(enc.URL, "magnet:") {
				r.Magnet = enc.URL
			} else if enc.URL != "" {
				r.TorrentURL = enc.URL
			}
			if enc.Length > 0 && r.SizeBytes == 0 {
				r.SizeBytes = enc.Length
			}
		}

		var catNum int
		for _, attr := range item.Attributes {
			switch attr.Name {
			case "magneturl":
				if attr.Value != "" {
					r.Magnet = attr.Value
				}
			case "seeders":
				r.Seeders, _ = strconv.Atoi(attr.Value)
			case "peers":
				n, _ := strconv.Atoi(attr.Value)
				r.Leechers = n
			case "leechers":
				if n, err := strconv.Atoi(attr.Value); err == nil {
					r.Leechers = n
				}
			case "size":
				if n, err := strconv.ParseInt(attr.Value, 10, 64); err == nil && n > 0 {
					r.SizeBytes = n
				}
			case "infohash":
				r.Infohash = strings.ToLower(strings.TrimSpace(attr.Value))
			case "category":
				if n, err := strconv.Atoi(attr.Value); err == nil && catNum == 0 {
					catNum = n
				}
			case "coverurl", "poster":
				if r.PosterURL == "" {
					r.PosterURL = attr.Value
				}
			}
		}

		// peers in torznab is usually total; approximate leechers = peers - seeders.
		if r.Leechers >= r.Seeders && r.Seeders > 0 {
			r.Leechers -= r.Seeders
		}

		if r.Magnet == "" && strings.HasPrefix(item.Link, "magnet:") {
			r.Magnet = item.Link
		} else if r.TorrentURL == "" && strings.HasPrefix(item.Link, "http") {
			r.TorrentURL = item.Link
		}
		if r.Infohash == "" {
			r.Infohash = infohashFromMagnet(r.Magnet)
		}
		if catNum > 0 {
			r.Category = categoryFromTorznab(catNum)
		}
		if r.Magnet == "" && r.TorrentURL == "" {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// torznabEndpoint normalises a user-entered indexer URL to the torznab API
// endpoint that answers ?t=search / ?t=caps. Handles bare hosts, plain torznab
// servers (…/api) and Jackett (…/results/torznab/).
func torznabEndpoint(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty indexer URL")
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid indexer URL: %v", err)
	}
	p := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(p, "/api"):
		// already an API endpoint
	case strings.HasSuffix(p, "/torznab"):
		p += "/api"
	case p == "":
		p = "/api"
	default:
		p += "/api"
	}
	u.Path = p
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

// searchHTTPClient returns an HTTP client that honours CoreConfig.proxyURL
// (SOCKS5/HTTP) so tracker/indexer requests do not leak the device's real IP.
// proxyMode "peers" means "don't proxy HTTP", matching the BitTorrent side.
func searchHTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}

	if sets.Args != nil && sets.Args.ProxyURL != "" {
		mode := sets.Args.ProxyMode
		if mode == "" {
			mode = "tracker"
		}
		if mode != "peers" {
			if pu, err := url.Parse(sets.Args.ProxyURL); err == nil {
				switch pu.Scheme {
				case "http", "https":
					tr.Proxy = http.ProxyURL(pu)
				case "socks5", "socks5h":
					var auth *proxy.Auth
					if pu.User != nil {
						pw, _ := pu.User.Password()
						auth = &proxy.Auth{User: pu.User.Username(), Password: pw}
					}
					if d, derr := proxy.SOCKS5("tcp", pu.Host, auth, proxy.Direct); derr == nil {
						tr.Proxy = nil
						if cd, ok := d.(proxy.ContextDialer); ok {
							tr.DialContext = cd.DialContext
						} else {
							tr.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
								return d.Dial(network, addr)
							}
						}
					} else {
						log.TLogln("search: bad SOCKS proxy, using direct:", derr)
					}
				default:
					// socks4/socks4a and anything else: fall back to direct.
					log.TLogln("search: unsupported proxy scheme for HTTP, using direct:", pu.Scheme)
				}
			}
		}
	}

	return &http.Client{Timeout: timeout, Transport: tr}
}

func dedupResults(in []SearchResult) []SearchResult {
	seen := make(map[string]int, len(in))
	out := make([]SearchResult, 0, len(in))
	for _, r := range in {
		key := dedupKey(r)
		if idx, ok := seen[key]; ok {
			// Keep the richer / higher-seeded copy.
			if r.Seeders > out[idx].Seeders {
				merged := r
				if merged.PosterURL == "" {
					merged.PosterURL = out[idx].PosterURL
				}
				if merged.Magnet == "" {
					merged.Magnet = out[idx].Magnet
				}
				if merged.TorrentURL == "" {
					merged.TorrentURL = out[idx].TorrentURL
				}
				out[idx] = merged
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, r)
	}
	return out
}

func dedupKey(r SearchResult) string {
	if r.Infohash != "" {
		return "ih:" + strings.ToLower(r.Infohash)
	}
	if r.Magnet != "" {
		if ih := infohashFromMagnet(r.Magnet); ih != "" {
			return "ih:" + ih
		}
		return "mag:" + r.Magnet
	}
	if r.TorrentURL != "" {
		return "url:" + r.TorrentURL
	}
	return "title:" + strings.ToLower(r.Title) + ":" + strconv.FormatInt(r.SizeBytes, 10)
}

func infohashFromMagnet(magnet string) string {
	if !strings.HasPrefix(magnet, "magnet:") {
		return ""
	}
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	for _, xt := range u.Query()["xt"] {
		if strings.HasPrefix(xt, "urn:btih:") {
			return strings.ToLower(strings.TrimPrefix(xt, "urn:btih:"))
		}
		if strings.HasPrefix(xt, "urn:btmh:") {
			return strings.ToLower(strings.TrimPrefix(xt, "urn:btmh:"))
		}
	}
	return ""
}

// torznabCatsFor maps the app's category enum to torznab numeric category ids.
// A per-indexer override list (already numeric strings) wins when present.
func torznabCatsFor(category string, override []string) string {
	if len(override) > 0 {
		return strings.Join(override, ",")
	}
	switch category {
	case "movies":
		return "2000"
	case "tv":
		return "5000"
	case "anime":
		return "5070,2000"
	case "music":
		return "3000"
	case "video":
		return "2000,5000"
	case "other":
		return "8000"
	default: // all
		return ""
	}
}

func categoryFromTorznab(cat int) string {
	switch {
	case cat == 5070:
		return "anime"
	case cat >= 2000 && cat < 3000:
		return "movies"
	case cat >= 5000 && cat < 6000:
		return "tv"
	case cat >= 3000 && cat < 4000:
		return "music"
	default:
		return "other"
	}
}

func categoryFromRutor(cats string) string {
	c := strings.ToLower(cats)
	switch {
	case strings.Contains(c, "anime"):
		return "anime"
	case strings.Contains(c, "series"), strings.Contains(c, "tvshow"):
		return "tv"
	case strings.Contains(c, "movie"):
		return "movies"
	default:
		return "video"
	}
}

// matchesCategory decides whether an aggregated result passes the active filter.
// "video" is an umbrella over movies / tv / anime / bare video.
func matchesCategory(resultCat, filter string) bool {
	if filter == "all" || filter == "" {
		return true
	}
	if resultCat == filter {
		return true
	}
	if filter == "video" {
		switch resultCat {
		case "movies", "tv", "anime", "video":
			return true
		}
	}
	return false
}

func parseRSSDate(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

// parseHumanSize turns "12.4 GB" / "700 MiB" / "1,5 ГБ" into bytes.
func parseHumanSize(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, ",", ".")
	s = strings.ReplaceAll(s, " ", " ")

	var num strings.Builder
	rest := ""
	for i, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			num.WriteRune(r)
			continue
		}
		rest = strings.TrimSpace(s[i:])
		break
	}
	val, err := strconv.ParseFloat(num.String(), 64)
	if err != nil {
		return 0
	}

	mult := float64(1)
	switch {
	case strings.HasPrefix(rest, "к"), strings.HasPrefix(rest, "k"):
		mult = 1 << 10
	case strings.HasPrefix(rest, "м"), strings.HasPrefix(rest, "m"):
		mult = 1 << 20
	case strings.HasPrefix(rest, "г"), strings.HasPrefix(rest, "g"):
		mult = 1 << 30
	case strings.HasPrefix(rest, "т"), strings.HasPrefix(rest, "t"):
		mult = 1 << 40
	}
	return int64(val * mult)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
