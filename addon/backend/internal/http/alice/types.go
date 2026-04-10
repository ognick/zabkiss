package alice

import (
	"context"
)

const version = "1.0"

type aliceResponse struct {
	Response         responseBody   `json:"response"`
	SessionState     map[string]any `json:"session_state,omitempty"`
	UserStateUpdate  map[string]any `json:"user_state_update,omitempty"`
	ApplicationState map[string]any `json:"application_state,omitempty"`
	Analytics        *analytics     `json:"analytics,omitempty"`
	Version          string         `json:"version"`
}

type responseBody struct {
	Text       string      `json:"text"`
	TTS        string      `json:"tts,omitempty"`
	Card       any         `json:"card,omitempty"`
	Buttons    []button    `json:"buttons,omitempty"`
	EndSession bool        `json:"end_session"`
	Directives *directives `json:"directives,omitempty"`
}

type button struct {
	Title   string         `json:"title"`
	URL     string         `json:"url,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
	Hide    bool           `json:"hide,omitempty"`
}

type directives struct {
	StartAccountLinking *struct{} `json:"start_account_linking,omitempty"`
}

// ── Cards ─────────────────────────────────────────────────────────────────────

type bigImageCard struct {
	Type        string      `json:"type"` // "BigImage"
	ImageID     string      `json:"image_id"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Button      *cardButton `json:"button,omitempty"`
}

type itemsListCard struct {
	Type   string           `json:"type"` // "ItemsList"
	Header *itemsListHeader `json:"header,omitempty"`
	Items  []listItem       `json:"items"`
	Footer *itemsListFooter `json:"footer,omitempty"`
}

type imageGalleryCard struct {
	Type  string        `json:"type"` // "ImageGallery"
	Items []galleryItem `json:"items"`
}

type itemsListHeader struct {
	Text string `json:"text"`
}

type listItem struct {
	ImageID     string      `json:"image_id"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Button      *cardButton `json:"button,omitempty"`
}

type itemsListFooter struct {
	Text   string      `json:"text"`
	Button *cardButton `json:"button,omitempty"`
}

type galleryItem struct {
	ImageID string      `json:"image_id"`
	Title   string      `json:"title"`
	Button  *cardButton `json:"button,omitempty"`
}

type cardButton struct {
	Text    string         `json:"text,omitempty"`
	URL     string         `json:"url,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// ── Analytics ─────────────────────────────────────────────────────────────────

type analytics struct {
	Events []analyticsEvent `json:"events,omitempty"`
}

type analyticsEvent struct {
	Name  string         `json:"name"`
	Value map[string]any `json:"value,omitempty"`
}

// ── Alice request ─────────────────────────────────────────────────────────────

type aliceRequest struct {
	Session struct {
		SessionID string `json:"session_id"`
		MessageID int    `json:"message_id"`
User      struct {
			UserID      string `json:"user_id"`
			AccessToken string `json:"access_token"`
		} `json:"user"`
	} `json:"session"`
	Request struct {
		Command           string `json:"command"`
		OriginalUtterance string `json:"original_utterance"`
	} `json:"request"`
}

type aliceWebhook func(ctx context.Context, req aliceRequest) (aliceResponse, error)
