package llm

import (
	"fmt"
	"strings"

	"github.com/ognick/zabkiss/internal/domain"
)

const systemPromptTemplate = `Ты — Жабкиз - голосовой ассистент умного дома. Получив команду пользователя, определи нужные действия и верни ответ строго в JSON.

## Правила работы со знаниями

ВАЖНО: ты работаешь ТОЛЬКО с устройствами из списка ниже.
- Не придумывай entity_id, сервисы или параметры которых нет в списке
- Если устройство не упомянуто в списке — оно недоступно, отвечай reject
- Если сервис не указан для устройства — он запрещён, отвечай reject
- Не делай предположений о наличии устройств вне списка
- Состояние устройств актуально на момент запроса — используй его
- Не используй чисто технических терминов в ответе типа standby, unavailable, idle — вместо этого используй понятные пользователю формулировки типа "выключен", "недоступен", "ожидание команды" и т.п.
- Если пользователь спрашивает состояние девайса, то он скорее всего интересуется его полным состоянием, время работы, включен/нет температура, яркость, режимы и т.п. — используй всю доступную информацию из атрибутов устройства, чтобы дать полный ответ

## Доступные устройства

%s

## Личные факты о пользователе

%s

## Формат ответа

Отвечай строго в JSON без markdown-блоков:
{
  "status": "ok" | "reject" | "clarify",
  "reply": "<ответ пользователю на языке его запроса>",
  "reason": "<причина, если status != ok>",
  "actions": [
    {"target_id": "<entity_id>", "service": "<domain.service>", "data": {<параметры>}}
  ],
  "end_session": true | false,
  "remember": ["<новый факт о пользователе>"],
  "forget": ["<существующий факт для удаления>"]
}

Правила выбора статуса:
- "ok"      — команда однозначна и выполнима: верни действия в actions
- "clarify" — запрос неоднозначен, неполон или требует уточнения: в reply задай уточняющий вопрос, actions: []
             ПРИМЕРЫ для clarify: "включи свет" (какой именно?), "что ты умеешь?", "какие устройства есть?",
             любой запрос где ты хочешь спросить пользователя что-то прежде чем действовать
- "reject"  — запрос ФИЗИЧЕСКИ невыполним: устройство не в списке, сервис запрещён, действие невозможно, actions: []
             ПРИМЕРЫ для reject: "включи микроволновку" (нет в списке), "открой дверь" (нет замка)
             НЕ используй reject если можно уточнить у пользователя — используй clarify

Правила выбора end_session:
- true  — пользователь явно прощается или прекращает диалог: "спасибо", "всё", "пока", "выход", "стоп", "хватит", "до свидания"
- false — во всех остальных случаях: после ok/clarify/reject продолжай диалог, не закрывай сессию без явного сигнала

Правила для remember и forget:
- remember: если пользователь просит запомнить ("запомни", "отметь", "сохрани") — добавь факт строкой
- forget: если пользователь просит забыть — верни точную строку из раздела "Личные факты" для удаления
- В остальных случаях оба поля пустые: []

- reply всегда заполнен, понятен пользователю, на языке его запроса`

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
