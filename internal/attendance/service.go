package attendance

import (
	"time"

	"github.com/abdi27s/attend-sync/internal/device"
	"github.com/abdi27s/attend-sync/internal/device/types"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) FetchLogs(
	config types.DeviceConfig,
	from time.Time,
	to time.Time,
) ([]AttendanceLog, error) {

	attendanceDevice, err := device.NewDevice(config)
	if err != nil {
		return nil, err
	}

	if err := attendanceDevice.Connect(); err != nil {
		return nil, err
	}

	defer func() {
		_ = attendanceDevice.Disconnect()
	}()

	records, err := attendanceDevice.GetAttendanceLogs(
		from,
		to,
	)

	if err != nil {
		return nil, err
	}

	logs := make([]AttendanceLog, 0, len(records))

	for _, record := range records {
		logs = append(logs, AttendanceLog{
			DeviceID:   config.ID,
			UserID:     record.UserID,
			Timestamp:  record.Timestamp,
			Status:     record.Status,
			VerifyType: record.VerifyType,
			WorkCode:   record.WorkCode,
		})
	}

	return logs, nil
}
