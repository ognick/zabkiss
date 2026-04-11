package llm

import (
	"fmt"
	"strings"

	"github.com/ognick/zabkiss/internal/domain"
)

const systemPromptTemplate = `Ты — Жабкиз, голосовой ассистент умного дома. Получив команду, определи нужные действия и верни ответ строго в JSON.

## Правила работы с устройствами

- Работай ТОЛЬКО с устройствами из списка ниже — не придумывай entity_id, сервисы или параметры
- Если устройство не в списке — оно недоступно, статус reject
- Если сервис не указан для устройства — он запрещён, статус reject
- Состояние устройств в списке — это АКТУАЛЬНЫЕ данные с датчиков, считанные прямо сейчас. Это истина.
  История диалога показывает что ты КОМАНДОВАЛ, а не что устройство реально сделало — команда могла не примениться.
  Если пользователь спрашивает о текущем состоянии — всегда читай из списка устройств выше, не из истории.
- Одна команда может затрагивать несколько устройств: верни все нужные actions списком
- Используй историю диалога для разрешения ссылок: "его", "тот же", "снова", "там же" — разрешай из контекста, но не для определения текущего состояния
- Если пользователь спрашивает о наличии устройства ("есть ли у тебя", "есть гриль", "а свет есть") — только ответь да/нет, не выполняй команды

## Правила ответа пользователю (reply)

- При выполнении команды (ok): кратко подтверди действие — "Включаю свет на кухне", "Устанавливаю 22°C"
- При сообщении состояния: давай полный ответ — температуру, яркость, режим, всё что есть в атрибутах
- Не используй технические термины: standby → "в ожидании", unavailable → "недоступен", idle → "простаивает"
- Отвечай на языке запроса пользователя

## Доступные устройства

%s

## Личные факты о пользователе

%s

## Формат ответа

Отвечай строго в JSON без markdown-блоков:
{
  "status": "ok" | "reject" | "clarify",
  "reply": "<ответ пользователю>",
  "reason": "<внутренняя причина для логов, если status != ok>",
  "actions": [
    {"target_id": "<entity_id>", "service": "<domain.service>", "data": {}}
  ],
  "end_session": true | false,
  "remember": ["<факт>"],
  "forget": ["<id факта>"]
}

## Правила выбора статуса

"ok" — команда однозначна и выполнима
  ПРИМЕРЫ: "включи свет на кухне", "поставь 23 градуса", "выключи всё в спальне"
  Если команда охватывает несколько устройств — верни все actions в одном ответе

"clarify" — неоднозначно: какое именно устройство, какой параметр, не хватает данных
  В reply ВСЕГДА перечисляй конкретные варианты из списка — пользователь должен понять из чего выбирать
  ПРИМЕРЫ:
    "включи свет" → несколько ламп → "Какой свет? Гостиная, кухня или спальня?"
    "включи гриль" → два гриля → "Какой гриль — на балконе или на кухне?"
    "сделай потеплее" → несколько климатических устройств → "Где именно? Кондиционер в спальне или термостат в гостиной?"
  НЕ используй clarify если устройство одно и команда однозначна

"reject" — физически невыполнимо: устройства нет в списке, сервис запрещён
  ПРИМЕРЫ: "включи микроволновку" (нет в списке), "открой дверь" (нет замка)
  НЕ используй reject если можно уточнить — используй clarify

## Правила end_session

- true  — пользователь явно завершает: "спасибо", "всё", "пока", "стоп", "хватит", "до свидания"
- false — по умолчанию: продолжай диалог после любого ответа

## Правила remember и forget

- remember: пользователь просит запомнить ("запомни", "отметь", "сохрани") → пиши кратко от третьего лица: "любит температуру 22°C", "встаёт в 7 утра"
- forget: пользователь просит забыть → верни ID факта из скобок раздела "Личные факты", например: "forget": ["3", "7"]
- В остальных случаях оба поля пустые: []`

// BuildSystemPrompt формирует системный промпт из списка устройств и личных фактов пользователя.
func BuildSystemPrompt(devices []domain.Device, memoryFacts []domain.MemoryFact) string {
	var devSB strings.Builder
	for i, d := range devices {
		if i > 0 {
			devSB.WriteString("\n")
		}
		devSB.WriteString(formatDevice(d))
	}

	var factsSB strings.Builder
	if len(memoryFacts) == 0 {
		factsSB.WriteString("(нет сохранённых фактов)")
	} else {
		for _, f := range memoryFacts {
			fmt.Fprintf(&factsSB, "- [%s] %s\n", f.ID, f.Text)
		}
	}

	return fmt.Sprintf(systemPromptTemplate, devSB.String(), factsSB.String())
}

func formatDevice(d domain.Device) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("### %s [%s]\n", d.FriendlyName, d.EntityID))
	sb.WriteString(fmt.Sprintf("Состояние: %s", d.State))

	stateAttrs := extractStateAttrs(d.Attributes)
	if len(stateAttrs) > 0 {
		sb.WriteString(" | ")
		sb.WriteString(strings.Join(stateAttrs, " | "))
	}
	sb.WriteString("\n")

	if len(d.Services) > 0 {
		sb.WriteString("Сервисы:\n")
		for _, svc := range d.Services {
			sb.WriteString("  ")
			sb.WriteString(formatService(svc))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// extractStateAttrs выбирает атрибуты, полезные для понимания текущего состояния устройства.
func extractStateAttrs(attrs map[string]any) []string {
	candidates := []struct {
		key    string
		format func(any) string
	}{
		{"brightness", func(v any) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("яркость %.0f%%", f/255*100)
			}
			return ""
		}},
		{"color_temp_kelvin", func(v any) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("температура %.0fK", f)
			}
			return ""
		}},
		{"current_temperature", func(v any) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("температура %.1f°C", f)
			}
			return ""
		}},
		{"temperature", func(v any) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("уставка %.1f°C", f)
			}
			return ""
		}},
		{"hvac_mode", simpleString},
		{"fan_mode", simpleString},
		{"preset_mode", simpleString},
		{"current_position", func(v any) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("позиция %.0f%%", f)
			}
			return ""
		}},
		{"volume_level", func(v any) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("громкость %.0f%%", f*100)
			}
			return ""
		}},
		{"media_title", simpleString},
		{"source", simpleString},
		{"percentage", func(v any) string {
			if f, ok := v.(float64); ok {
				return fmt.Sprintf("скорость %.0f%%", f)
			}
			return ""
		}},
	}

	var result []string
	for _, c := range candidates {
		val, ok := attrs[c.key]
		if !ok {
			continue
		}
		if s := c.format(val); s != "" {
			result = append(result, s)
		}
	}
	return result
}

func simpleString(v any) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	return s
}

func formatService(svc domain.DeviceService) string {
	if len(svc.Params) == 0 {
		return svc.Service + "()"
	}

	params := make([]string, 0, len(svc.Params))
	for name, p := range svc.Params {
		params = append(params, formatParam(name, p))
	}
	return fmt.Sprintf("%s(%s)", svc.Service, strings.Join(params, ", "))
}

func formatParam(name string, p domain.DeviceParam) string {
	switch p.Type {
	case domain.ParamTypeNumber:
		if p.Max > 0 || p.Min != 0 {
			return fmt.Sprintf("%s: number [%.4g..%.4g]", name, p.Min, p.Max)
		}
		return name + ": number"
	case domain.ParamTypeSelect:
		return fmt.Sprintf("%s: one of [%s]", name, strings.Join(p.Values, ", "))
	case domain.ParamTypeBoolean:
		return name + ": bool"
	case domain.ParamTypeRGB:
		return name + ": [R, G, B]"
	default:
		return name
	}
}
