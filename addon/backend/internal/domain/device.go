package domain

// ParamType описывает тип параметра сервиса.
type ParamType string

const (
	ParamTypeNumber  ParamType = "number"
	ParamTypeSelect  ParamType = "select"
	ParamTypeBoolean ParamType = "boolean"
	ParamTypeRGB     ParamType = "rgb"
	ParamTypeString  ParamType = "string"
)

// DeviceParam описывает один параметр сервиса с ограничениями.
type DeviceParam struct {
	Type   ParamType
	Min    float64  // для Number
	Max    float64  // для Number
	Values []string // для Select
}

// DeviceService — сервис, применимый к устройству.
type DeviceService struct {
	Service string              // e.g. "light.turn_on"
	Params  map[string]DeviceParam // имя параметра → описание
}

// Device — состояние устройства + применимые сервисы (доменная модель).
type Device struct {
	EntityID     string
	FriendlyName string
	State        string
	Attributes   map[string]any
	Services     []DeviceService
}
