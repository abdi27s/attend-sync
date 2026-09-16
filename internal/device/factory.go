package device

import (
	"fmt"
	"strings"

	"github.com/abdi27s/attend-sync/internal/device/types"
	"github.com/abdi27s/attend-sync/internal/device/zkteco"
)

func New(config types.DeviceConfig) (Device, error) {
	if config.Host == "" {
		return nil, fmt.Errorf("device host is required")
	}

	switch strings.ToLower(strings.TrimSpace(config.Type)) {
	case "zkteco":
		return zkteco.New(config), nil

	default:
		return nil, fmt.Errorf("unsupported device type: %q (supported: zkteco)", config.Type)
	}
}
