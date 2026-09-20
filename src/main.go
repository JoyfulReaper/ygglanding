package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	listenAddress = "[201:762f:80bd:20e1:20db:1239:19af:f25e]:8083"
	missionURL    = "http://10.99.0.1:5190/api/events"
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

func main() {
	apiKey := os.Getenv("MISSION_CONTROL_API_KEY")

	fileServer := http.FileServer(http.Dir("./static"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)

		if apiKey != "" {
			go publishVisit(apiKey, r)
		}
	})

	log.Printf("Ygg landing listening on http://%s", listenAddress)
	log.Fatal(http.ListenAndServe(listenAddress, nil))
}

func publishVisit(apiKey string, r *http.Request) {
	remote := r.RemoteAddr

	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remote = host
	}

	event := missionEvent{
		EventID:       newID(),
		EventType:     "ygglanding.request.completed",
		SchemaVersion: 1,
		OccurredAt:    time.Now().UTC(),
		Payload: visitPayload{
			Remote:    remote,
			Method:    r.Method,
			Path:      r.URL.Path,
			UserAgent: r.UserAgent(),
		},
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

	client := &http.Client{
		Timeout: time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("telemetry publish failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("telemetry rejected: HTTP %d", resp.StatusCode)
	}
}

func newID() string {
	var b [16]byte

	if _, err := rand.Read(b[:]); err != nil {
		return strings.Repeat("0", 32)
	}

	// UUID v4 bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	s := hex.EncodeToString(b[:])

	return s[0:8] + "-" +
		s[8:12] + "-" +
		s[12:16] + "-" +
		s[16:20] + "-" +
		s[20:32]
}
