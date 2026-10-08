package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"nineguard/internal/db"
	"nineguard/internal/handler"
	"nineguard/internal/traffic"
)

func TestUsageReportAndStatsAcceptTZ(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "tz.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tm := traffic.NewManager(database)
	h := handler.New(nil, nil, tm, nil, nil, nil, nil, "")

	// A row 1 minute after Jakarta midnight today, which is 17:01 UTC "yesterday".
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(jkt)
	jktMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 1, 0, 0, jkt).UTC()
	if _, err := database.Exec(`INSERT INTO traffic_logs (timestamp, api_key, api_key_name, model, status_code, client_ip)
		VALUES (?, 'sk-ng-...abcd', 'pi-dev', 'm', 200, '127.0.0.1')`, jktMidnight.Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		tz   string
		want int
	}{{"Asia/Jakarta", 1}, {"Not/AZone", -1}} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/report?period=today&tz="+tc.tz, nil)
		w := httptest.NewRecorder()
		h.GetUsageReport(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("tz=%s: status %d %s", tc.tz, w.Code, w.Body.String())
		}
		var rep traffic.UsageReport
		if err := json.NewDecoder(w.Body).Decode(&rep); err != nil {
			t.Fatal(err)
		}
		if tc.want >= 0 && rep.TotalRequests != tc.want {
			t.Errorf("tz=%s: total %d, want %d", tc.tz, rep.TotalRequests, tc.want)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/stats?period=today&tz=Asia/Jakarta", nil)
	w := httptest.NewRecorder()
	h.GetTrafficStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stats status %d", w.Code)
	}
	var stats traffic.DashboardStats
	_ = json.NewDecoder(w.Body).Decode(&stats)
	if stats.TotalRequests != 1 {
		t.Errorf("stats jakarta today: %d, want 1", stats.TotalRequests)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/traffic?period=today&tz=Asia/Jakarta", nil)
	w = httptest.NewRecorder()
	h.GetTrafficLogs(w, req)
	var logs struct {
		Total int `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&logs)
	if logs.Total != 1 {
		t.Errorf("traffic logs jakarta today: %d, want 1", logs.Total)
	}
}
