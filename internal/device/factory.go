package device

import (
	"fmt"
	"strings"

	"github.com/abdi27s/attend-sync/internal/device/types"
	"github.com/abdi27s/attend-sync/internal/device/zkteco"
)

func New(config types.DeviceConfig) (Device, error) {
	switch strings.ToLower(config.Type) {
	case "zkteco":
		return zkteco.New(config), nil

	default:
		return nil, fmt.Errorf("unsupported device type: %s", config.Type)
	}
}
