package attendance

import "time"

type AttendanceLog struct {
	DeviceID   string    `json:"device_id"`
	UserID     string    `json:"user_id"`
	Timestamp  time.Time `json:"timestamp"`
	Status     string    `json:"status"`
	VerifyType string    `json:"verify_type"`
	WorkCode   string    `json:"work_code"`
}
