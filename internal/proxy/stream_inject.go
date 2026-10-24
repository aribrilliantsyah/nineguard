package proxy

import (
	"bytes"
	"encoding/json"
)

// InjectStreamUsage inspects an OpenAI chat completion JSON request body.
// If stream_options.include_usage is not already true, it injects include_usage: true.
// Preserves all unknown fields in the JSON body.
// Returns the updated body and true if injection occurred.
func InjectStreamUsage(body []byte) ([]byte, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		// Not a JSON object; pass through unmodified
		return body, false, nil
	}

	optsRaw, hasOpts := raw["stream_options"]
	if hasOpts {
		var opts map[string]interface{}
		if err := json.Unmarshal(optsRaw, &opts); err == nil && opts != nil {
			if inc, ok := opts["include_usage"].(bool); ok && inc {
				// Already requested by client; nothing to inject
				return body, false, nil
			}
			opts["include_usage"] = true
			updatedOpts, err := json.Marshal(opts)
			if err != nil {
				return body, false, err
			}
			raw["stream_options"] = updatedOpts
			updatedBody, err := json.Marshal(raw)
			if err != nil {
				return body, false, err
			}
			return updatedBody, true, nil
		}
	}

	// stream_options absent or invalid object; set stream_options = {"include_usage": true}
	opts := map[string]interface{}{"include_usage": true}
	updatedOpts, err := json.Marshal(opts)
	if err != nil {
		return body, false, err
	}
	raw["stream_options"] = updatedOpts
	updatedBody, err := json.Marshal(raw)
	if err != nil {
		return body, false, err
	}
	return updatedBody, true, nil
}

// IsUsageOnlySSEEvent inspects a raw SSE event (bytes ending in double newline).
// It returns true if the event contains a JSON object where "choices" is an empty array
// and "usage" is a non-null object.
// It also returns the trimmed payload of the "data:" line(s).
func IsUsageOnlySSEEvent(event []byte) (bool, []byte) {
	lines := bytes.Split(event, []byte("\n"))
	var dataLines [][]byte
	for _, line := range lines {
		trimmed := bytes.TrimRight(line, "\r")
		if bytes.HasPrefix(trimmed, []byte("data: ")) {
			dataLines = append(dataLines, bytes.TrimSpace(trimmed[6:]))
		} else if bytes.HasPrefix(trimmed, []byte("data:")) {
			dataLines = append(dataLines, bytes.TrimSpace(trimmed[5:]))
		}
	}
	if len(dataLines) == 0 {
		return false, nil
	}
	payload := bytes.Join(dataLines, []byte("\n"))
	if bytes.Equal(payload, []byte("[DONE]")) {
		return false, payload
	}

	var inspect struct {
		Choices []json.RawMessage `json:"choices"`
		Usage   *json.RawMessage  `json:"usage"`
	}
	if err := json.Unmarshal(payload, &inspect); err != nil {
		return false, payload
	}

	isUsageOnly := len(inspect.Choices) == 0 && inspect.Usage != nil
	return isUsageOnly, payload
}
