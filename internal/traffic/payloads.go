package traffic

import (
	"database/sql"
	"time"
)

type PayloadEntry struct {
	TrafficID    int64     `json:"traffic_id"`
	RequestBody  string    `json:"request_body"`
	ResponseBody string    `json:"response_body"`
	CreatedAt    time.Time `json:"created_at"`
}

const maxPayloadBytes = 512 * 1024 // 512KB cap per body

func clampBytes(b []byte) []byte {
	if len(b) > maxPayloadBytes {
		return b[:maxPayloadBytes]
	}
	return b
}

func (m *Manager) SavePayload(trafficID int64, reqBody, respBody []byte) error {
	if m == nil || m.db == nil || trafficID <= 0 {
		return nil
	}
	reqClamped := clampBytes(reqBody)
	respClamped := clampBytes(respBody)

	_, err := m.db.Exec(`
		INSERT INTO traffic_payloads (traffic_id, request_body, response_body, created_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(traffic_id) DO UPDATE SET
			request_body = excluded.request_body,
			response_body = excluded.response_body
	`, trafficID, reqClamped, respClamped)
	return err
}

func (m *Manager) GetPayload(trafficID int64) (*PayloadEntry, error) {
	if m == nil || m.db == nil {
		return nil, sql.ErrNoRows
	}
	var p PayloadEntry
	var reqBytes, respBytes []byte
	err := m.db.QueryRow(`
		SELECT traffic_id, request_body, response_body, created_at
		FROM traffic_payloads
		WHERE traffic_id = ?
	`, trafficID).Scan(&p.TrafficID, &reqBytes, &respBytes, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.RequestBody = string(reqBytes)
	p.ResponseBody = string(respBytes)
	return &p, nil
}

func (m *Manager) PurgeOldPayloads(maxAge time.Duration) (int64, error) {
	if m == nil || m.db == nil {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-maxAge).Format("2006-01-02 15:04:05")
	res, err := m.db.Exec("DELETE FROM traffic_payloads WHERE created_at < datetime(?)", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (m *Manager) GetRecordPayloadsSetting() string {
	if m == nil || m.db == nil {
		return "disabled"
	}
	var val string
	err := m.db.QueryRow("SELECT value FROM settings WHERE key = 'record_payloads' LIMIT 1").Scan(&val)
	if err == nil && val != "" {
		return val
	}
	return "disabled"
}

func (m *Manager) SetRecordPayloadsSetting(val string) error {
	if m == nil || m.db == nil {
		return nil
	}
	_, err := m.db.Exec(`
		INSERT INTO settings (key, value, updated_at)
		VALUES ('record_payloads', ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, val)
	return err
}
