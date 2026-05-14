package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type RenderMode struct {
	Query     map[string]string
	Subdomain string
}

type Resolver struct {
	Url         string
	UptimeStart time.Time
	Latency     time.Duration
	IsUp        bool
	LastChecked time.Time
	Normal      *RenderMode
	Gallery     *RenderMode
	Direct      *RenderMode
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

func (r Resolver) Ping() time.Duration {
	resUrl, err := url.Parse(r.Url)
	if err != nil {
		errorLog.Fatalf("Invalid URL for %s : %v", resUrl, err)
	}
	domainName := resUrl.Hostname()

	ip, err := net.LookupIP(domainName)
	if err != nil {
		errorLog.Printf("Cannot resolve %s to an IP address: %v", domainName, err)
	}
	targetIP := ip[0]
	listener, err := icmp.ListenPacket("udp4", "0.0.0.0") // Set up a listener to catch the ICMP echo requests back
	if err != nil {
		errorLog.Printf("listen error: %v", err)
	}
	defer listener.Close()

	wm := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{
			ID: os.Getpid() & 0xffff, Seq: 1,
			Data: []byte("abcdefghijklmnopqrstuvwxyz"),
		},
	}
	wb, err := wm.Marshal(nil)
	if err != nil {
		errorLog.Fatal(err)
	}

	start := time.Now()
	if _, err := listener.WriteTo(wb, &net.UDPAddr{IP: targetIP, Port: 1}); err != nil {
		errorLog.Fatalf("WriteTo error, %s", err)
	}

	_ = listener.SetReadDeadline(time.Now().Add(1 * time.Second))
	rb := make([]byte, 1500)
	_, peer, err := listener.ReadFrom(rb)
	if err != nil {
		errorLog.Printf("Could not retrieve the ping echo back. Remote host '%s' might be blocking ICMP ping requests, or is offline: %v", peer, err)
		return 0
	}
	diff := time.Since(start)

	return diff
}

func (r Resolver) ResolveEmbed(id string) string {
	client := &http.Client{}
	// Forge the URL with the video ID
	var targetUrl = r.Url + "/p/" + id
	req, err := http.NewRequest("GET", targetUrl, nil)
	if err != nil {
		errorLog.Fatalf("Error invoking the get request: %v", err)
	}
	// Mimic discord's user-agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)")

	// Send request ..
	resp, err := client.Do(req)
	if err != nil {
		errorLog.Fatalf("Error invoking the http GET request: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		errorLog.Fatalf("Error reading response body: %v", err)
	}

	return string(bodyBytes)

}
