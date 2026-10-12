package registry

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
)

type HeartbeatWorker struct {
	registry   *PresenceRegistry
	httpClient *http.Client
}

func NewHeartbeatWorker(r *PresenceRegistry) *HeartbeatWorker {
	return &HeartbeatWorker{
		registry: r,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (w *HeartbeatWorker) Start(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			w.CheckAll()
		}
	}()
}

func (w *HeartbeatWorker) CheckAll() {
	records := w.registry.GetAllRecords()
	for _, record := range records {
		if record.InternalKey == "" {
			continue
		}

		// Currently only Yggdrasil-API uses internal key and has heartbeat
		// In a real multi-service environment, we might need a way to resolve the service URL
		// For now, we assume if it's the Yggdrasil API service, we know how to reach it.
		// A better way is to use the relay_url if available in presence record.
		// However, presence record doesn't store the base URL yet.
		// For now, we only support the Yggdrasil API specifically as requested.
		
		if record.Name == config.AppConfig.OAuth2.SuperClientID {
			w.checkService(record)
		}
	}
}

func (w *HeartbeatWorker) checkService(record PresenceRecord) {
	// Use Yggdrasil base URL from config for the heartbeat check
	url := fmt.Sprintf("%s/internal/heartbeat", config.AppConfig.YggdrasilAPI.BaseURL)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("[CORE-HB] Failed to create request for %s: %v", record.Name, err)
		return
	}
	req.Header.Set("X-Internal-Key", record.InternalKey)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		log.Printf("[CORE-HB] Heartbeat failed for %s: %v. Revoking key.", record.Name, err)
		w.registry.RevokeAuthKey(record.Name)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[CORE-HB] Heartbeat returned status %d for %s. Revoking key.", resp.StatusCode, record.Name)
		w.registry.RevokeAuthKey(record.Name)
	}
}
