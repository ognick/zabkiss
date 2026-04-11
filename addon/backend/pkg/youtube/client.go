package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const searchURL = "https://www.googleapis.com/youtube/v3/search"

// SearchResult — результат поиска видео на YouTube.
type SearchResult struct {
	VideoID string
	Title   string
}

// Client — клиент YouTube Data API v3.
type Client struct {
	apiKey string
	http   *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{apiKey: apiKey, http: http.DefaultClient}
}

// SearchVideo ищет первое видео по запросу и возвращает его ID и заголовок.
func (c *Client) SearchVideo(ctx context.Context, query string) (SearchResult, error) {
	params := url.Values{
		"part":       {"snippet"},
		"type":       {"video"},
		"maxResults": {"1"},
		"q":          {query},
		"key":        {c.apiKey},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL+"?"+params.Encode(), nil)
	if err != nil {
		return SearchResult{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return SearchResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return SearchResult{}, fmt.Errorf("youtube search: HTTP %s", resp.Status)
	}

	var body struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return SearchResult{}, fmt.Errorf("youtube search decode: %w", err)
	}
	if len(body.Items) == 0 {
		return SearchResult{}, fmt.Errorf("youtube search: no results for %q", query)
	}
	return SearchResult{
		VideoID: body.Items[0].ID.VideoID,
		Title:   body.Items[0].Snippet.Title,
	}, nil
}
