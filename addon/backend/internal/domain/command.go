package domain

// CommandStatus — результат обработки команды.
type CommandStatus string

const (
	CommandOK      CommandStatus = "ok"      // команда выполнена
	CommandReject  CommandStatus = "reject"  // выполнить невозможно
	CommandClarify CommandStatus = "clarify" // нужно уточнение у пользователя
)

// ChatMessage — одно сообщение в истории диалога.
type ChatMessage struct {
	Role    string // "user" or "assistant"
	Content string
}

// Action — одна команда для Home Assistant.
type Action struct {
	TargetID string         // entity_id
	Service  string         // domain.service, e.g. "light.turn_on"
	Data     map[string]any // параметры сервиса
}

// CommandResult — результат выполнения голосовой команды.
type CommandResult struct {
	Status     CommandStatus
	Reply      string   // что произносится вслух
	Actions    []Action // действия для HA (пусто при reject/clarify)
	EndSession bool     // завершить ли сессию Алисы
}
