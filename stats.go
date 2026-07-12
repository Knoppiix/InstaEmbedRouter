package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const prometheusURL = "http://localhost:9090"

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}      `json:"value"`  // instant query: [timestamp, "value"]
			Values [][]interface{}    `json:"values"` // range query: [][timestamp, "value"]
		} `json:"result"`
	} `json:"data"`
}

type statPoint struct {
	Resolver string  `json:"resolver"`
	Value    float64 `json:"value"`
}

type seriesPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

func queryPrometheusInstant(promQuery string) (*promResponse, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", prometheusURL, url.QueryEscape(promQuery))
	return doPromRequest(u)
}

func queryPrometheusRange(promQuery string, start, end time.Time, step string) (*promResponse, error) {
	u := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=%s",
		prometheusURL, url.QueryEscape(promQuery), start.Unix(), end.Unix(), step)
	return doPromRequest(u)
}

func doPromRequest(u string) (*promResponse, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var pr promResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, err
	}
	if pr.Status != "success" {
		return nil, fmt.Errorf("prometheus query failed: %s", string(body))
	}
	return &pr, nil
}

func parseValue(raw []interface{}) float64 {
	if len(raw) != 2 {
		return 0
	}
	s, ok := raw[1].(string)
	if !ok {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// GET /api/stats/success -> total successful embeds per resolver (instaembed_resolver_success_total)
func statsSuccessHandler(w http.ResponseWriter, r *http.Request) {
	pr, err := queryPrometheusInstant("instaembed_resolver_success_total")
	if err != nil {
		errorLog.Printf("stats/success: %v", err)
		http.Error(w, "failed to query prometheus", http.StatusBadGateway)
		return
	}

	points := []statPoint{}
	for _, res := range pr.Data.Result {
		if v := parseValue(res.Value); v > 0 {
			points = append(points, statPoint{Resolver: res.Metric["resolver"], Value: v})
		}
	}

	writeJSON(w, points)
}

// GET /api/stats/latency -> average latency per resolver over the last 24h, for resolvers that served traffic
func statsLatencyHandler(w http.ResponseWriter, r *http.Request) {
	query := `rate(instaembed_resolver_latency_seconds_sum[24h]) / (rate(instaembed_resolver_latency_seconds_count[24h])) and on(resolver) rate(instaembed_resolver_latency_seconds_count[24h]) > 0`
	pr, err := queryPrometheusInstant(query)
	if err != nil {
		errorLog.Printf("stats/latency: %v", err)
		http.Error(w, "failed to query prometheus", http.StatusBadGateway)
		return
	}

	points := []statPoint{}
	for _, res := range pr.Data.Result {
		points = append(points, statPoint{Resolver: res.Metric["resolver"], Value: parseValue(res.Value)})
	}

	writeJSON(w, points)
}

// GET /api/stats/requests-timeseries -> incoming requests per 10min bucket, over the last 24h
func statsRequestsTimeseriesHandler(w http.ResponseWriter, r *http.Request) {
	end := time.Now()
	start := end.Add(-24 * time.Hour)
	pr, err := queryPrometheusRange("increase(instaembed_requests_total[10m])", start, end, "10m")
	if err != nil {
		errorLog.Printf("stats/requests-timeseries: %v", err)
		http.Error(w, "failed to query prometheus", http.StatusBadGateway)
		return
	}

	points := []seriesPoint{}
	if len(pr.Data.Result) > 0 {
		for _, v := range pr.Data.Result[0].Values {
			ts, ok := v[0].(float64)
			if !ok {
				continue
			}
			valStr, ok := v[1].(string)
			if !ok {
				continue
			}
			val, _ := strconv.ParseFloat(valStr, 64)
			points = append(points, seriesPoint{Timestamp: int64(ts), Value: val})
		}
	}

	writeJSON(w, points)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
