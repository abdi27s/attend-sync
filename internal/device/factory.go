package device

import (
	"fmt"

	"github.com/abdi27s/attend-sync/internal/device/types"
	"github.com/abdi27s/attend-sync/internal/device/zkteco"
)

func NewDevice(config types.DeviceConfig) (types.AttendanceDevice, error) {
	switch config.Type {
	case "zkteco":
		return zkteco.New(config), nil

	// case "hikvision":
	// 	return hikvision.New(config), nil

	// case "suprema":
	// 	return suprema.New(config), nil

	// case "anviz":
	// 	return anviz.New(config), nil

	default:
		return nil, fmt.Errorf(
			"unsupported device type: %s",
			config.Type,
		)
	}
}
