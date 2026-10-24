package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
)

var knownCloakSuffixes = []string{"_ide", "_cc"}

// ExtractOfferedTools parses incoming request JSON and extracts declared tool names into a set.
//   - Anthropic (messages): tools[].name
//   - OpenAI (chat): tools[].function.name and functions[].name
func ExtractOfferedTools(apiFormat string, body []byte) map[string]bool {
	offered := make(map[string]bool)
	if len(body) == 0 {
		return offered
	}

	var req map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&req); err != nil {
		return offered
	}

	if apiFormat == "messages" {
		if tools, ok := req["tools"].([]interface{}); ok {
			for _, t := range tools {
				if tMap, ok := t.(map[string]interface{}); ok {
					if name, ok := tMap["name"].(string); ok && name != "" {
						offered[name] = true
					}
				}
			}
		}
	} else {
		if tools, ok := req["tools"].([]interface{}); ok {
			for _, t := range tools {
				if tMap, ok := t.(map[string]interface{}); ok {
					if fnMap, ok := tMap["function"].(map[string]interface{}); ok {
						if name, ok := fnMap["name"].(string); ok && name != "" {
							offered[name] = true
						}
					} else if name, ok := tMap["name"].(string); ok && name != "" {
						offered[name] = true
					}
				}
			}
		}
		if fns, ok := req["functions"].([]interface{}); ok {
			for _, f := range fns {
				if fMap, ok := f.(map[string]interface{}); ok {
					if name, ok := fMap["name"].(string); ok && name != "" {
						offered[name] = true
					}
				}
			}
		}
	}

	return offered
}

// StripCloakSuffix strips known cloaking suffixes (_ide, _cc).
//   - If offered[name] is true, return name as-is (preserves tools legitimately named with _ide).
//   - Iteratively strip known suffixes while the name ends with them. If any intermediate stripped
//     name exists in offered, return that matched name immediately.
//   - Handles single cloaking (read_ide -> read) and compounding double cloaking (read_ide_ide -> read).
func StripCloakSuffix(name string, offered map[string]bool) string {
	if len(offered) > 0 && offered[name] {
		return name
	}

	cur := name
	for {
		stripped := false
		for _, suffix := range knownCloakSuffixes {
			if len(cur) > len(suffix) && strings.HasSuffix(cur, suffix) {
				cur = strings.TrimSuffix(cur, suffix)
				stripped = true
				if len(offered) > 0 && offered[cur] {
					return cur
				}
				break
			}
		}
		if !stripped {
			break
		}
	}

	if len(offered) == 0 {
		return cur
	}
	return name
}

// CleanRequestToolHistory de-poisons message history before sending upstream.
//   - If Anthropic (messages): in messages[] where role is assistant, if content blocks have
//     type: "tool_use", decloak name if it has cloaking suffix and matched in offered.
//     Also check tool_choice.name if present.
//   - If OpenAI (chat): in messages[] where role is assistant, decloak tool_calls[].function.name.
//
// Returns updated JSON bytes (or original bytes if unchanged).
func CleanRequestToolHistory(apiFormat string, body []byte, offered map[string]bool) []byte {
	if len(body) == 0 || len(offered) == 0 {
		return body
	}

	var req map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		return body
	}

	changed := false

	if apiFormat == "messages" {
		if msgs, ok := req["messages"].([]interface{}); ok {
			for _, m := range msgs {
				if mMap, ok := m.(map[string]interface{}); ok {
					if role, _ := mMap["role"].(string); role == "assistant" {
						if contentList, ok := mMap["content"].([]interface{}); ok {
							for _, block := range contentList {
								if bMap, ok := block.(map[string]interface{}); ok {
									if bType, _ := bMap["type"].(string); bType == "tool_use" {
										if name, ok := bMap["name"].(string); ok && name != "" {
											decloaked := StripCloakSuffix(name, offered)
											if decloaked != name {
												bMap["name"] = decloaked
												changed = true
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
		if tc, ok := req["tool_choice"].(map[string]interface{}); ok {
			if name, ok := tc["name"].(string); ok && name != "" {
				decloaked := StripCloakSuffix(name, offered)
				if decloaked != name {
					tc["name"] = decloaked
					changed = true
				}
			}
		}
	} else {
		if msgs, ok := req["messages"].([]interface{}); ok {
			for _, m := range msgs {
				if mMap, ok := m.(map[string]interface{}); ok {
					role, _ := mMap["role"].(string)
					if role == "assistant" {
						if tcList, ok := mMap["tool_calls"].([]interface{}); ok {
							for _, tc := range tcList {
								if tcMap, ok := tc.(map[string]interface{}); ok {
									if fnMap, ok := tcMap["function"].(map[string]interface{}); ok {
										if name, ok := fnMap["name"].(string); ok && name != "" {
											decloaked := StripCloakSuffix(name, offered)
											if decloaked != name {
												fnMap["name"] = decloaked
												changed = true
											}
										}
									}
								}
							}
						}
						if fcMap, ok := mMap["function_call"].(map[string]interface{}); ok {
							if name, ok := fcMap["name"].(string); ok && name != "" {
								decloaked := StripCloakSuffix(name, offered)
								if decloaked != name {
									fcMap["name"] = decloaked
									changed = true
								}
							}
						}
					} else if role == "function" {
						if name, ok := mMap["name"].(string); ok && name != "" {
							decloaked := StripCloakSuffix(name, offered)
							if decloaked != name {
								mMap["name"] = decloaked
								changed = true
							}
						}
					}
				}
			}
		}
		if tc, ok := req["tool_choice"].(map[string]interface{}); ok {
			if fnMap, ok := tc["function"].(map[string]interface{}); ok {
				if name, ok := fnMap["name"].(string); ok && name != "" {
					decloaked := StripCloakSuffix(name, offered)
					if decloaked != name {
						fnMap["name"] = decloaked
						changed = true
					}
				}
			} else if name, ok := tc["name"].(string); ok && name != "" {
				decloaked := StripCloakSuffix(name, offered)
				if decloaked != name {
					tc["name"] = decloaked
					changed = true
				}
			}
		}
	}

	if !changed {
		return body
	}

	rewritten, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return rewritten
}

// DecloakSSEEvent modifies streaming SSE events to decloak tool names.
//   - In messages format: if event is content_block_start with content_block.type == "tool_use":
//     Decloak content_block.name using StripCloakSuffix. Rebuild data payload JSON and replace data line.
//   - In chat format: if event has choices[].delta.tool_calls[].function.name matching a suffixed tool, decloak it.
//
// Returns the (possibly modified) rawEvent.
func DecloakSSEEvent(apiFormat string, rawEvent []byte, dataPayload []byte, offered map[string]bool) []byte {
	if len(rawEvent) == 0 || len(offered) == 0 {
		return rawEvent
	}

	if len(dataPayload) == 0 {
		_, dataPayload = IsUsageOnlySSEEvent(rawEvent)
	}
	if len(dataPayload) == 0 || bytes.Equal(dataPayload, []byte("[DONE]")) {
		return rawEvent
	}

	var payloadMap map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(dataPayload))
	dec.UseNumber()
	if err := dec.Decode(&payloadMap); err != nil {
		return rawEvent
	}

	changed := false

	if apiFormat == "messages" {
		if cb, ok := payloadMap["content_block"].(map[string]interface{}); ok {
			if cbType, _ := cb["type"].(string); cbType == "tool_use" {
				if name, ok := cb["name"].(string); ok && name != "" {
					decloaked := StripCloakSuffix(name, offered)
					if decloaked != name {
						cb["name"] = decloaked
						changed = true
					}
				}
			}
		}
	} else {
		if choices, ok := payloadMap["choices"].([]interface{}); ok {
			for _, c := range choices {
				if cMap, ok := c.(map[string]interface{}); ok {
					if delta, ok := cMap["delta"].(map[string]interface{}); ok {
						if tcList, ok := delta["tool_calls"].([]interface{}); ok {
							for _, tc := range tcList {
								if tcMap, ok := tc.(map[string]interface{}); ok {
									if fnMap, ok := tcMap["function"].(map[string]interface{}); ok {
										if name, ok := fnMap["name"].(string); ok && name != "" {
											decloaked := StripCloakSuffix(name, offered)
											if decloaked != name {
												fnMap["name"] = decloaked
												changed = true
											}
										}
									}
								}
							}
						}
						if fcMap, ok := delta["function_call"].(map[string]interface{}); ok {
							if name, ok := fcMap["name"].(string); ok && name != "" {
								decloaked := StripCloakSuffix(name, offered)
								if decloaked != name {
									fcMap["name"] = decloaked
									changed = true
								}
							}
						}
					}
				}
			}
		}
	}

	if !changed {
		return rawEvent
	}

	newPayload, err := json.Marshal(payloadMap)
	if err != nil {
		return rawEvent
	}

	return replaceSSEData(rawEvent, newPayload)
}

func replaceSSEData(rawEvent []byte, newPayload []byte) []byte {
	useCRLF := bytes.Contains(rawEvent, []byte("\r\n"))
	eol := []byte("\n")
	if useCRLF {
		eol = []byte("\r\n")
	}

	lines := bytes.Split(rawEvent, []byte("\n"))
	var newLines [][]byte
	dataReplaced := false

	for _, line := range lines {
		trimmed := bytes.TrimRight(line, "\r")
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			if !dataReplaced {
				newLine := append([]byte("data: "), newPayload...)
				newLines = append(newLines, newLine)
				dataReplaced = true
			}
		} else {
			newLines = append(newLines, trimmed)
		}
	}

	return bytes.Join(newLines, eol)
}

// DecloakResponseBody decloaks tool names in non-streaming JSON responses.
//   - In messages format: iterate content[], decloak tool_use names.
//   - In chat format: iterate choices[].message.tool_calls[], decloak function names.
func DecloakResponseBody(apiFormat string, respBody []byte, offered map[string]bool) []byte {
	if len(respBody) == 0 || len(offered) == 0 {
		return respBody
	}

	var respMap map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(respBody))
	dec.UseNumber()
	if err := dec.Decode(&respMap); err != nil {
		return respBody
	}

	changed := false

	if apiFormat == "messages" {
		if contentList, ok := respMap["content"].([]interface{}); ok {
			for _, block := range contentList {
				if bMap, ok := block.(map[string]interface{}); ok {
					if bType, _ := bMap["type"].(string); bType == "tool_use" {
						if name, ok := bMap["name"].(string); ok && name != "" {
							decloaked := StripCloakSuffix(name, offered)
							if decloaked != name {
								bMap["name"] = decloaked
								changed = true
							}
						}
					}
				}
			}
		}
	} else {
		if choices, ok := respMap["choices"].([]interface{}); ok {
			for _, choice := range choices {
				if cMap, ok := choice.(map[string]interface{}); ok {
					if msg, ok := cMap["message"].(map[string]interface{}); ok {
						if tcList, ok := msg["tool_calls"].([]interface{}); ok {
							for _, tc := range tcList {
								if tcMap, ok := tc.(map[string]interface{}); ok {
									if fnMap, ok := tcMap["function"].(map[string]interface{}); ok {
										if name, ok := fnMap["name"].(string); ok && name != "" {
											decloaked := StripCloakSuffix(name, offered)
											if decloaked != name {
												fnMap["name"] = decloaked
												changed = true
											}
										}
									}
								}
							}
						}
						if fcMap, ok := msg["function_call"].(map[string]interface{}); ok {
							if name, ok := fcMap["name"].(string); ok && name != "" {
								decloaked := StripCloakSuffix(name, offered)
								if decloaked != name {
									fcMap["name"] = decloaked
									changed = true
								}
							}
						}
					}
				}
			}
		}
	}

	if !changed {
		return respBody
	}

	rewritten, err := json.Marshal(respMap)
	if err != nil {
		return respBody
	}
	return rewritten
}

// PreserveThinkingIntent ensures reasoning_effort field is present in forwardBody
// if thinking object was specified by client in chat format, preventing 9router
// from stripping reasoning intent on tool turns.
func PreserveThinkingIntent(body []byte) []byte {
	var raw map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return body
	}

	thinkingVal, hasThinking := raw["thinking"]
	if !hasThinking || thinkingVal == nil {
		return body
	}

	thinkingMap, isObj := thinkingVal.(map[string]interface{})
	if !isObj {
		return body
	}

	// If thinking is explicitly disabled, do not inject reasoning_effort
	if tType, ok := thinkingMap["type"].(string); ok && strings.EqualFold(tType, "disabled") {
		return body
	}

	// If reasoning_effort is already present and non-empty, preserve as-is
	if re, ok := raw["reasoning_effort"].(string); ok && strings.TrimSpace(re) != "" {
		return body
	}

	effort := "medium"
	if eff, ok := thinkingMap["effort"].(string); ok && strings.TrimSpace(eff) != "" {
		effort = strings.TrimSpace(eff)
	} else if bt, ok := thinkingMap["budget_tokens"]; ok {
		var budget int64
		switch v := bt.(type) {
		case json.Number:
			budget, _ = v.Int64()
		case float64:
			budget = int64(v)
		}
		if budget > 8000 {
			effort = "high"
		} else if budget > 0 && budget <= 2000 {
			effort = "low"
		} else if budget > 0 {
			effort = "medium"
		}
	}

	raw["reasoning_effort"] = effort
	if rewritten, err := json.Marshal(raw); err == nil {
		return rewritten
	}
	return body
}
