package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	defaultLegacyListenAddress  = "[201:762f:80bd:20e1:20db:1239:19af:f25e]:8083"
	defaultServiceListenAddress = "[301:762f:80bd:20e1::10]:80"

	defaultMissionURL       = "http://127.0.0.1:5190/api/events"
	missionControlEventType = "ygglanding.visit"

	defaultQOTDURL = "http://[301:762f:80bd:20e1::70]/api/quotes/today"
	defaultGitURL  = "http://[301:762f:80bd:20e1::50]/api/github/activity?limit=2"
)

type missionEvent struct {
	EventID       string      `json:"eventId"`
	EventType     string      `json:"eventType"`
	SchemaVersion int         `json:"schemaVersion"`
	OccurredAt    time.Time   `json:"occurredAt"`
	CorrelationID *string     `json:"correlationId"`
	Payload       interface{} `json:"payload"`
}

type visitPayload struct {
	Remote    string `json:"remote"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	UserAgent string `json:"userAgent"`
}

var httpClient = &http.Client{
	Timeout: time.Second,
}

var widgetHTTPClient = &http.Client{
	Timeout: 3 * time.Second,
}

func main() {
	apiKey := os.Getenv("MISSION_CONTROL_API_KEY")

	missionURL := envOrDefault(
		"MISSION_CONTROL_URL",
		defaultMissionURL,
	)

	legacyListenAddress := envOrDefault(
		"YGGLANDING_LEGACY_LISTEN_ADDRESS",
		defaultLegacyListenAddress,
	)

	serviceListenAddress := envOrDefault(
		"YGGLANDING_SERVICE_LISTEN_ADDRESS",
		defaultServiceListenAddress,
	)

	if apiKey == "" {
		log.Printf("warning: MISSION_CONTROL_API_KEY is not set; telemetry disabled")
	}

	fileServer := http.FileServer(http.Dir("./static"))

	mux := http.NewServeMux()

	mux.HandleFunc("/api/widgets/qotd", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		proxyJSON(
			w,
			r,
			widgetHTTPClient,
			envOrDefault("YGGLANDING_QOTD_URL", defaultQOTDURL),
		)
	})

	mux.HandleFunc("/api/widgets/git", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		proxyJSON(
			w,
			r,
			widgetHTTPClient,
			envOrDefault("YGGLANDING_GIT_URL", defaultGitURL),
		)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)

		if apiKey != "" &&
			r.URL.Path == "/" &&
			r.Method == http.MethodGet {
			payload := makeVisitPayload(r)
			go publishVisit(apiKey, missionURL, payload)
		}
	})

	errCh := make(chan error, 2)

	go func() {
		log.Printf(
			"Ygg landing legacy listener: http://%s",
			legacyListenAddress,
		)

		errCh <- http.ListenAndServe(
			legacyListenAddress,
			mux,
		)
	}()

	go func() {
		log.Printf(
			"Ygg landing dedicated listener: http://%s",
			serviceListenAddress,
		)

		errCh <- http.ListenAndServe(
			serviceListenAddress,
			mux,
		)
	}()

	log.Fatal(<-errCh)
}

func envOrDefault(name string, fallback string) string {
	value := os.Getenv(name)

	if value == "" {
		return fallback
	}

	return value
}

func proxyJSON(
	w http.ResponseWriter,
	r *http.Request,
	client *http.Client,
	target string,
) {
	req, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodGet,
		target,
		nil,
	)
	if err != nil {
		http.Error(w, "upstream request unavailable", http.StatusBadGateway)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("widget upstream %s failed: %v", target, err)
		http.Error(w, "widget temporarily unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("widget upstream %s returned HTTP %d", target, resp.StatusCode)
		http.Error(w, "widget temporarily unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	if _, err := io.Copy(w, io.LimitReader(resp.Body, 1<<20)); err != nil {
		log.Printf("widget response copy failed: %v", err)
	}
}

func makeVisitPayload(r *http.Request) visitPayload {
	remote := r.RemoteAddr

	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remote = host
	}

	return visitPayload{
		Remote:    remote,
		Method:    r.Method,
		Path:      r.URL.Path,
		UserAgent: r.UserAgent(),
	}
}

func publishVisit(
	apiKey string,
	missionURL string,
	payload visitPayload,
) {
	eventID, err := newID()
	if err != nil {
		log.Printf("telemetry event ID generation failed: %v", err)
		return
	}

	event := missionEvent{
		EventID:       eventID,
		EventType:     missionControlEventType,
		SchemaVersion: 1,
		OccurredAt:    time.Now().UTC(),
		Payload:       payload,
	}

	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("telemetry marshal failed: %v", err)
		return
	}

	req, err := http.NewRequest(
		http.MethodPost,
		missionURL,
		bytes.NewReader(body),
	)
	if err != nil {
		log.Printf("telemetry request failed: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mission-Control-Key", apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("telemetry publish failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf(
			"telemetry rejected: HTTP %d",
			resp.StatusCode,
		)
	}
}

func newID() (string, error) {
	var b [16]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	// UUID v4 bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	s := hex.EncodeToString(b[:])

	return s[0:8] + "-" +
		s[8:12] + "-" +
		s[12:16] + "-" +
		s[16:20] + "-" +
		s[20:32], nil
}
