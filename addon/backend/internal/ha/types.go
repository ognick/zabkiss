package ha

import (
	"strings"

	"github.com/ognick/zabkiss/internal/domain"
)

// entityState — состояние устройства из /api/states (внутренний DTO).
type entityState struct {
	EntityID     string
	State        string
	FriendlyName string
	Attributes   map[string]any
}

func (e entityState) haDomain() string {
	parts := strings.SplitN(e.EntityID, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

// applicableService — сервис HA до маппинга в доменную модель (внутренний DTO).
type applicableService struct {
	Service string
	Params  map[string]param
}

// param — параметр сервиса HA (внутренний DTO).
type param struct {
	Type   domain.ParamType
	Min    float64
	Max    float64
	Values []string
}

// toDevice маппит внутренние DTO в доменную модель Device.
func toDevice(s entityState, svcs []applicableService) domain.Device {
	domSvcs := make([]domain.DeviceService, len(svcs))
	for i, svc := range svcs {
		domParams := make(map[string]domain.DeviceParam, len(svc.Params))
		for name, p := range svc.Params {
			domParams[name] = domain.DeviceParam{
				Type:   p.Type,
				Min:    p.Min,
				Max:    p.Max,
				Values: p.Values,
			}
		}
		domSvcs[i] = domain.DeviceService{
			Service: svc.Service,
			Params:  domParams,
		}
	}
	return domain.Device{
		EntityID:     s.EntityID,
		FriendlyName: s.FriendlyName,
		State:        s.State,
		Attributes:   s.Attributes,
		Services:     domSvcs,
	}
}
