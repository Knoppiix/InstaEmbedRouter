package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Knoppiix/InstagramEmbedResolver/metrics"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var errorLog = log.New(os.Stderr, "ERROR: ", log.LstdFlags)

var routes = []string{
	"/p/{id}",
	"/p/{id}/{$}",
	"/reels/{id}",
	"/reels/{id}/{$}",
	"/reel/{id}",
	"/reel/{id}/{$}",
	"/{username}/p/{id}",
	"/{username}/p/{id}/{$}",
	"/{username}/reel/{id}",
	"/{username}/reel/{id}/{$}",
	"/share/{id}",
	"/share/{id}/{$}",
}
var defaultRes Resolver

func startServer(resolvers []Resolver, port int) {
	template := template.Must(template.ParseFiles("templates/index.html"))
	// home page handler
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		template.Execute(w, "")
	})

	for _, route := range routes {
		mux.HandleFunc("GET "+route, reqHandler(resolvers))
	}
	// expose the prometheus metrics route
	mux.Handle("GET /metrics", promhttp.Handler())

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), mux))
}

func reqHandler(resolvers []Resolver) http.HandlerFunc {
	// handler function for proxifying the requests to the resolvers
	return func(w http.ResponseWriter, r *http.Request) {

		// If the request is from discord OR telegram, we proxify the request through the resolver
		ua := r.Header.Get("User-Agent")
		if strings.Contains(ua, "Discordbot") || strings.Contains(ua, "Telegram") {
			proxyRequest(w, r, resolvers)
			return
		}
		// Else, we simply redirect the user to the instagram post

		// Extract the img_index query parameter if it exists
		imgIndex := r.URL.Query().Get("img_index")
		var query string
		if imgIndex != "" {
			query = fmt.Sprintf("?img_index=%s", imgIndex)
		}
		// Construct the redirect URL
		redirectURL := "https://instagram.com" + r.URL.Path + query

		// Send HTTP 302 redirect
		http.Redirect(w, r, redirectURL, http.StatusFound)
		log.Printf("User redirection toward %s", redirectURL)
	}
}

func main() {
	port := flag.Int("p", 8080, "port to run the server on")
	flag.Parse()
	log.SetOutput(os.Stdout)
	resolvers, err := loadResolvers("resolvers.json")
	if err != nil {
		log.Fatalf("Error reading the file: %v", err)
	}
	metrics.Init()
	go monitorResolvers(resolvers)
	startServer(resolvers, *port)
}
