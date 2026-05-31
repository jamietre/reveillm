package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Adapter forwards a request to a target URL and returns the streaming response.
// The caller is responsible for closing the response body.
type Adapter interface {
	Forward(ctx context.Context, method, targetURL string, headers http.Header, body []byte, apiKey, model string) (*http.Response, error)
}

// OpenAIAdapter is the default Adapter. It substitutes the model field, sets auth,
// and returns the upstream response for the caller to stream.
type OpenAIAdapter struct {
	client *http.Client
}

func NewOpenAIAdapter(client *http.Client) *OpenAIAdapter {
	return &OpenAIAdapter{client: client}
}

func (a *OpenAIAdapter) Forward(ctx context.Context, method, targetURL string, headers http.Header, body []byte, apiKey, model string) (*http.Response, error) {
	rewritten, err := SubstituteModel(body, model)
	if err != nil {
		return nil, fmt.Errorf("substituting model: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(rewritten))
	if err != nil {
		return nil, err
	}

	req.Header = headers.Clone()
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Del("Authorization")
	}

	return a.client.Do(req)
}

// SubstituteModel replaces the "model" field in a JSON object body.
// Returns body unchanged if body is empty or not a JSON object.
func SubstituteModel(body []byte, model string) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, nil
	}
	modelJSON, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	obj["model"] = modelJSON
	return json.Marshal(obj)
}

var _ Adapter = (*OpenAIAdapter)(nil)
