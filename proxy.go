package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Knoppiix/InstagramEmbedResolver/metrics"
	"github.com/PuerkitoBio/goquery"
)

var metaProps = map[string]bool{
	"og:video":              true,
	"og:video:secure_url":   true,
	"twitter:player:stream": true,
	"og:image":              true,
	"twitter:card":          true,
	"twitter:image":         true,
}

// Compiled once at startup: this used to be rebuilt on every single resolver
// response, which was a needless allocation on the hot path.
var cfScriptRe = regexp.MustCompile(`(?s)<script[^>]+static\.cloudflareinsights\.com[^<]+</script>`)

// Shared across all requests so the connections to the resolvers are actually
// pooled. DefaultTransport only keeps 2 idle conns per host, which is too low
// when every incoming request fans out to every resolver at once.
var resolverClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Redirects between resolvers are legitimate (they mirror and rebrand
		// each other constantly), so we only refuse the one that matters: a
		// bounce back to Instagram means the resolver gave up on the post.
		// Following it would have us parse Instagram's own page, find its
		// og:image, and serve that as a valid answer.
		if strings.TrimPrefix(req.URL.Hostname(), "www.") == "instagram.com" {
			return fmt.Errorf("redirected back to instagram.com (post unavailable, or user agent refused)")
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	},
}

// Hard ceiling on how long a single incoming request may fan out for. Nothing
// in this handler is allowed to outlive it.
const proxyTimeout = 5 * time.Second

type resolverResponse struct {
	resolverURL string
	startTime   time.Time
	hasVideo    bool
	hasImage    bool
	payload     *goquery.Document
	contentType string
	// Set instead of payload when the resolver served the media itself rather than
	// an HTML page. The client gets redirected here.
	streamURL string
}

// Losing the race is the normal case, not an error: as soon as one resolver
// answers with a video, proxyRequest returns and cancels the context, aborting
// every other in-flight request. Same thing when the client hangs up. Logging
// those would drown the real failures.
func logResolverError(resolverURL string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	log.Printf("Error with resolver %s: %v", resolverURL, err)
}

// A resolver that doesn't announce HTML is serving the media stream itself.
// A missing or malformed Content-Type is treated as HTML, which is what the
// resolvers that don't set the header at all were already getting.
func isHTMLResponse(contentType string) bool {
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return true
	}
	return mediaType == "text/html" || mediaType == "application/xhtml+xml"
}

func proxyRequest(w http.ResponseWriter, req *http.Request, resolvers []Resolver) {
	// Cancelling on return also aborts the in-flight requests of the resolvers
	// whose answer we no longer care about.
	ctx, cancel := context.WithTimeout(req.Context(), proxyTimeout)
	defer cancel()

	// The subdomain picks the render mode and doesn't depend on the resolvers, so
	// an unknown one is settled before fanning out.
	if !isServedRoute(req) {
		sendErrorEmbed(w, "Route not supported")
		return
	}

	// Only the resolvers we actually query may be awaited below. Counting
	// len(resolvers) instead would block the handler forever as soon as one
	// resolver is down, leaking the goroutine, the parsed documents it holds
	// and the client connection along with it.
	up := make([]Resolver, 0, len(resolvers))

  for _, resolver := range resolvers {
		if resolver.IsUp {
			up = append(up, resolver)
		}
	}
	if len(up) == 0 {
		sendErrorEmbed(w, "No embed service available")
		return
	}

	resultCh := make(chan resolverResponse, len(up))
	var resolversResponses []resolverResponse

	for _, resolver := range up {
		go sendReqToResolver(ctx, req, resolver, resultCh)
	}

	for i := 0; i < len(up); i++ {
		var response resolverResponse
		select {
		case <-ctx.Done():
			// Either we hit proxyTimeout or the client hung up.
			sendErrorEmbed(w, "Embed services timed out")
			return
		case response = <-resultCh:
		}

		//log.Printf("Got a response from %s", response.resolverURL)
		if response.hasVideo {
			serveResolverResponse(w, req, "video", response)
			return
		} else if response.hasImage {
			// We don’t instantly send the response to the client here, because some Instagram Reels are sometimes wrongly
			// embedded as images from some resolvers (when the scraper can’t fetch the video correctly, it only displays a thumbnail)
			// So it’s more reliable to wait for other resolvers’ responses to be sure it's a picture post
			resolversResponses = append(resolversResponses, response)
		}
	}

	if len(resolversResponses) > 0 {
		serveResolverResponse(w, req, "post", resolversResponses[0])
		return
	}

	sendErrorEmbed(w, "Post not found")
}

func sendReqToResolver(ctx context.Context, req *http.Request, resolver Resolver, resultCh chan resolverResponse) {
	response := resolverResponse{}
	// we guarantee the channel to always have at least 1 response (in case of early exits)
	// so it doesnt crash the receiver. The channel has one slot per goroutine, so
	// this send never blocks, even when the receiver already gave up.
	defer func() { resultCh <- response }()

	response.resolverURL = resolver.Url
	u, err := url.Parse(resolver.Url)
	if err != nil {
		return
	}

	// if using a specific mode (gallery, media-only) we check if the current resolver is able to handle it
	renderMode := getRenderMode(req, resolver)
	if renderMode == nil {
		log.Printf("The current resolver is not compatible with the request mode.")
		return
	}

	q := url.Values{}
	if imgIndex := req.URL.Query().Get("img_index"); imgIndex != "" {
		q.Set("img_index", imgIndex)
	}
	for k, v := range renderMode.Query {
		q.Set(k, v)
	}

	// if the user specified functional parameters (like ?image_index) we encode them
	query := ""
	if len(q) > 0 {
		query = "?" + q.Encode()
	}

	// "sanitizing" the URL so we don't carry additionnal parameters (like the ?igsh)
	fullUrl := u.Scheme + "://" + renderMode.Subdomain + u.Host + req.URL.Path + query
	response.startTime = time.Now()

	outReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullUrl, nil)
	if err != nil {
		log.Printf("Error building the request for resolver %s: %v", resolver.Url, err)
		return
	}
	outReq.Header.Set("User-Agent", resolver.effectiveUserAgent())

	resp, err := resolverClient.Do(outReq)
	if err != nil {
		logResolverError(resolver.Url, err)
		return
	}

	// Some resolvers don't answer with an HTML page carrying og: tags, they serve
	// the raw media stream directly. Reading that body would pull the whole video
	// into memory just to hand it to an HTML parser, so we stop here and only keep
	// the URL to bounce the client to.
	if !isHTMLResponse(resp.Header.Get("Content-Type")) {
		resp.Body.Close()
		// The URL after any redirect the client followed, so Discord makes one hop
		// less than we did.
		response.streamURL = resp.Request.URL.String()
		response.hasVideo = true
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		// Cancellation can also land here, while the body is still being read.
		logResolverError(resolver.Url, err)
		return
	}

	doc, err := rewriteAndCleanDoc(bodyBytes, resolver.Url)
	if err != nil {
		log.Printf("Error building the document from %s: %v", resolver.Url, err)
		return
	}
	response.payload = doc

	tags := getMetaTags(doc)

	// If the tags found in the resolver's reponse contain a video (twitter:player:stream or og:video:secure_url for
	// example)
	for _, tag := range tags {
		if strings.Contains(tag, "video") || strings.Contains(tag, "stream") {
			response.hasVideo = true
		}
		if strings.Contains(tag, "image") {
			response.hasImage = true
		}
	}
	response.contentType = resp.Header.Get("Content-Type")

}

// Discord builds its embed from the og: tags and paints the bar on the embed's
// left with theme-color, which is a name= and not a property= since it isn't an
// OpenGraph tag. og:title is what makes Discord render an embed at all.
const errorEmbedColor = "#ED4245" // Discord's own red

var errorEmbedTmpl = template.Must(template.New("errorEmbed").Parse(
	`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta property="og:title" content="Embed failed">
<meta property="og:description" content="{{.}}">
<meta name="theme-color" content="` + errorEmbedColor + `">
</head>
<body>{{.}}</body>
</html>
`))

// Sends the failure as an embeddable page rather than a bare status text, so the
// reason shows up in Discord instead of a blank link. Status stays 200: Discordbot
// doesn't build an embed out of an error response, so the failure is carried by
// the embed's content, not by the code.
func sendErrorEmbed(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := errorEmbedTmpl.Execute(w, reason); err != nil {
		errorLog.Printf("rendering the error embed: %v", err)
	}
}

// Hands the winning resolver's answer to the client, either as the rewritten
// HTML page or, when the resolver served the media itself, as a redirect to it.
func serveResolverResponse(w http.ResponseWriter, req *http.Request, kind string, response resolverResponse) {
	if response.streamURL != "" {
		// Discordbot follows redirects, so pointing it straight at the stream is
		// enough to get the video embedded, and the bytes never transit through us.
		http.Redirect(w, req, response.streamURL, http.StatusFound)
		log.Printf("[stream] Redirected to %s from %s for https://instagram.com%s", response.streamURL, response.resolverURL, req.URL.Path)
	} else {
		if err := sendRespToClient(w, response.contentType, response.payload); err != nil {
			return
		}
		log.Printf("[%s] Sent response from %s for https://instagram.com%s", kind, response.resolverURL, req.URL.Path)
	}
	// prometheus metrics ..
	recordResolverMetrics(response)
}

func sendRespToClient(w http.ResponseWriter, contentType string, doc *goquery.Document) error {
	html, err := doc.Html()
	if err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		return err
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
	return nil
}

func getMetaTags(doc *goquery.Document) []string {
	var tags []string

	doc.Find("meta").Each(func(i int, s *goquery.Selection) {
		if prop, ok := s.Attr("property"); ok {
			tags = append(tags, prop)
		}
		if name, ok := s.Attr("name"); ok {
			tags = append(tags, name)
		}
	})

	return tags
}

// isServedRoute reports whether the request's subdomain selects a render mode we
// know about. It says nothing about whether a given resolver supports that mode.
func isServedRoute(req *http.Request) bool {
	switch getSubdomain(req.Host) {
	case "g", "d", "n", "tst", "":
		return true
	}
	return false
}

func getRenderMode(req *http.Request, res Resolver) *RenderMode {
	if !isServedRoute(req) {
		return nil
	}

	switch getSubdomain(req.Host) {
	case "g":
		return res.Gallery
	case "d":
		return res.Direct
	case "n":
		return res.Normal
	}
	// "tst" or no subdomain at all: used for tests only
	return &RenderMode{}
}

func getSubdomain(host string) string {
	host = strings.Split(host, ":")[0]

	parts := strings.Split(host, ".")
	if len(parts) < 3 || parts[0] == "www" {
		return "" // no subdomain
	}
	return parts[0]
}

// Cleaning the HTML body and replacing the relative URLs from the meta video tags to absolute paths
// because relative paths do not work through a proxifier
func rewriteAndCleanDoc(bodyBytes []byte, resolverURL string) (*goquery.Document, error) {
	// Removing cloudflare's <script> tags that might break the embedding
	bodyBytes = cfScriptRe.ReplaceAll(bodyBytes, nil)

	// Parse with goquery
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	// Compute origin (scheme + host)
	u, _ := url.Parse(resolverURL)
	origin := u.Scheme + "://" + u.Host

	// I'm not sure the base tag is interpreted by discord's embedding scrapper. But in my tests, it SEEMS to work better with it.
	doc.Find("head").PrependHtml(`<base href="` + origin + `">`)

	// relative path to absolute path
	doc.Find("meta").Each(func(i int, s *goquery.Selection) {
		if prop, ok := s.Attr("property"); ok && metaProps[prop] {
			if val, ok := s.Attr("content"); ok && strings.HasPrefix(val, "/") {
				s.SetAttr("content", origin+val)
			}
		}
		//the meta property twitter:player:stream may use "name" instead of "property"
		if name, ok := s.Attr("name"); ok && metaProps[name] {
			if val, ok := s.Attr("content"); ok && strings.HasPrefix(val, "/") {
				s.SetAttr("content", origin+val)
			}
		}
	})

	return doc, nil
}

func recordResolverMetrics(resp resolverResponse) {
	metrics.TotalRequests.Inc()
	metrics.SuccessfulEmbeds.WithLabelValues(resp.resolverURL).Inc()

	latency := time.Since(resp.startTime).Seconds()
	metrics.ResolverRequests.WithLabelValues(resp.resolverURL).Inc()
	metrics.ResolverLatency.WithLabelValues(resp.resolverURL).Observe(latency)
}
