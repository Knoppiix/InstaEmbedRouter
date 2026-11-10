package main

import (
	"context"
	"net/http"
	"strings"
	"time"
)

type RenderMode struct {
	Query     map[string]string
	Subdomain string
}

// User agent used when proxying to a resolver that doesn't set its own in
// resolvers.json. We mimic Discord because that's the client we're proxying for,
// and the resolvers tailor their output to it.
const defaultUserAgent = "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)"

type Resolver struct {
	Url         string
	UserAgent   string
	UptimeStart time.Time
	Latency     time.Duration
	IsUp        bool
	LastChecked time.Time
	Normal      *RenderMode
	Gallery     *RenderMode
	Direct      *RenderMode
}

// effectiveUserAgent falls back to the default when the "UserAgent" key is
// absent from resolvers.json. Going through this instead of reading the field
// directly also avoids sending an empty User-Agent header: net/http treats an
// explicitly-set empty value as "send no user agent at all", which most
// resolvers reject.
func (r Resolver) effectiveUserAgent() string {
	if r.UserAgent == "" {
		return defaultUserAgent
	}
	return r.UserAgent
}

func (r *Resolver) IsHttpUp() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// mimic regular user agent to avoid possible antibots
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/116.0.0.0 Safari/537.36"

	req, err := http.NewRequestWithContext(ctx, "GET", r.Url, nil)
	if err != nil {
		return false, err
	}

	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Referer", "https://www.instagram.com/")

	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	diff := time.Since(start)
	r.Latency = diff

	// Treat any 2xx as up
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Read a small prefix to check content-type / html presence without loading the whole body
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		prefix := strings.ToLower(string(buf[:n]))
		ct := strings.ToLower(resp.Header.Get("Content-Type"))

		// If the Content-Type indicates HTML or the body prefix contains HTML tags, it's likely a valid page
		if strings.Contains(ct, "text/html") || strings.Contains(prefix, "<html") || strings.Contains(prefix, "<!doctype") {
			return true, nil
		}
		return true, nil
	}

	// non-2xx status -> treat as down
	return false, nil
}
