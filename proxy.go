package main

import (
	"bytes"
	"io"
	"log"
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

type resolverResponse struct {
	resolverURL string
	startTime   time.Time
	hasVideo    bool
	hasImage    bool
	payload     *goquery.Document
	contentType string
}

func proxyRequest(w http.ResponseWriter, req *http.Request, resolvers []Resolver) {
	client := &http.Client{Timeout: 2 * time.Second}
	resultCh := make(chan resolverResponse, len(resolvers))
	var resolversResponses []resolverResponse

	for _, resolver := range resolvers {
		go sendReqToResolver(req, client, resolver, resultCh)
	}

	for i := 0; i < len(resolvers); i++ {
		response := <-resultCh
		//log.Printf("Got a response from %s", response.resolverURL)
		if response.hasVideo {
			sendRespToClient(w, response.contentType, response.payload)
			log.Printf("[video] Sent response from %s for https://instagram.com%s", response.resolverURL, req.URL.Path)
			// prometheus metrics ..
			recordResolverMetrics(response)
			return
		} else if response.hasImage {
			resolversResponses = append(resolversResponses, response)
		}
	}

	if len(resolversResponses) > 0 {
		response := resolversResponses[0]
		// TODO: probably a code smell to have these lines dupplicated
		sendRespToClient(w, response.contentType, response.payload)
		log.Printf("[post] Sent response from %s for https://instagram.com%s", response.resolverURL, req.URL.Path)
		recordResolverMetrics(response)
		return
	}

	// TODO: send error to client (that would embed cleanly, to display an error message)
	http.Error(w, `Post not found`, http.StatusBadGateway)
}

func sendReqToResolver(req *http.Request, client *http.Client, resolver Resolver, resultCh chan resolverResponse) {
	response := resolverResponse{}
	// we guarantee the channel to always have at least 1 response (in case of early exits)
	// so it doesnt crash the receiver
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
	resp, err := client.Get(fullUrl)
	if err != nil {
		log.Printf("Error with resolver %s: %v", resolver.Url, err)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Printf("Error reading body from %s: %v", resolver.Url, err)
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

func getRenderMode(req *http.Request, res Resolver) *RenderMode {
	host := req.Host
	host = strings.Split(host, ":")[0] // get rid of the port
	sub := getSubdomain(host)

	switch sub {
	case "g":
		return res.Gallery
	case "d":
		return res.Direct
	case "n":
		return res.Normal
	case "tst", "":
		// subdomain used for tests only
		return &RenderMode{}
	}
	return nil
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
	// Searching for cloudflare's <script> tags that might break the embedding

	re := regexp.MustCompile(`(?s)<script[^>]+static\.cloudflareinsights\.com[^<]+</script>`)
	// And we're removing it
	bodyBytes = re.ReplaceAll(bodyBytes, nil)

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
