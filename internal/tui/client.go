package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client wraps HTTP calls to the management API.
type Client struct {
	baseURL   string
	secretKey string
	http      *http.Client
}

// NewClient creates a new management API client.
func NewClient(port int, secretKey string) *Client {
	return &Client{
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		secretKey: strings.TrimSpace(secretKey),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SetSecretKey updates management API bearer token used by this client.
func (c *Client) SetSecretKey(secretKey string) {
	c.secretKey = strings.TrimSpace(secretKey)
}

func (c *Client) doRequest(method, path string, body io.Reader) ([]byte, int, error) {
	url := c.baseURL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, err
	}
	if c.secretKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.secretKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

func (c *Client) get(path string) ([]byte, error) {
	data, code, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", code, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *Client) put(path string, body io.Reader) ([]byte, error) {
	data, code, err := c.doRequest("PUT", path, body)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", code, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *Client) patch(path string, body io.Reader) ([]byte, error) {
	data, code, err := c.doRequest("PATCH", path, body)
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", code, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// getJSON fetches a path and unmarshals JSON into a generic map.
func (c *Client) getJSON(path string) (map[string]any, error) {
	data, err := c.get(path)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// postJSON sends a JSON body via POST and checks for errors.
func (c *Client) postJSON(path string, body any) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, code, err := c.doRequest("POST", path, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("HTTP %d", code)
	}
	return nil
}

// GetConfig fetches the parsed config.
func (c *Client) GetConfig() (map[string]any, error) {
	return c.getJSON("/v0/management/config")
}

// GetConfigYAML fetches the raw config.yaml content.
func (c *Client) GetConfigYAML() (string, error) {
	data, err := c.get("/v0/management/config.yaml")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// PutConfigYAML uploads new config.yaml content.
func (c *Client) PutConfigYAML(yamlContent string) error {
	_, err := c.put("/v0/management/config.yaml", strings.NewReader(yamlContent))
	return err
}

// GetUsage fetches usage statistics.
func (c *Client) GetUsage() (map[string]any, error) {
	return c.getJSON("/v0/management/usage")
}

// GetCredentials lists credentials from the platform API.
func (c *Client) GetCredentials() ([]map[string]any, error) {
	return c.getPlatformAuthFiles()
}

// DeleteCredential deletes a single credential.
func (c *Client) DeleteCredential(file map[string]any) error {
	credentialID := getString(file, "credential_id")
	if credentialID == "" {
		return fmt.Errorf("missing credential_id")
	}
	_, code, err := c.doRequest("DELETE", "/v2/credentials/"+url.PathEscape(credentialID), nil)
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("delete failed (HTTP %d)", code)
	}
	return nil
}

// ToggleCredential enables or disables a credential.
func (c *Client) ToggleCredential(file map[string]any, disabled bool) error {
	credentialID := getString(file, "credential_id")
	if credentialID == "" {
		return fmt.Errorf("missing credential_id")
	}
	body, _ := json.Marshal(map[string]any{"disabled": disabled})
	_, err := c.patch("/v2/credentials/"+url.PathEscape(credentialID)+"/status", strings.NewReader(string(body)))
	return err
}

// PatchCredentialFields updates editable fields on a credential.
func (c *Client) PatchCredentialFields(file map[string]any, fields map[string]any) error {
	credentialID := getString(file, "credential_id")
	if credentialID == "" {
		return fmt.Errorf("missing credential_id")
	}
	raw, err := c.get("/v2/credentials/" + url.PathEscape(credentialID) + "/download")
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	for key, value := range fields {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.put("/v2/credentials/"+url.PathEscape(credentialID)+"/content", strings.NewReader(string(body)))
	return err
}

func (c *Client) GetCredentialDetail(file map[string]any) (map[string]any, error) {
	credentialID := getString(file, "credential_id")
	if credentialID == "" {
		return nil, fmt.Errorf("missing credential_id")
	}
	wrapper, err := c.getJSON("/v2/credentials/" + url.PathEscape(credentialID))
	if err != nil {
		return nil, err
	}
	return normalizePlatformCredentialDetail(wrapper), nil
}

// GetLogs fetches log lines from the server.
func (c *Client) GetLogs(after int64, limit int) ([]string, int64, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if after > 0 {
		query.Set("after", strconv.FormatInt(after, 10))
	}

	path := "/v0/management/logs"
	encodedQuery := query.Encode()
	if encodedQuery != "" {
		path += "?" + encodedQuery
	}

	wrapper, err := c.getJSON(path)
	if err != nil {
		return nil, after, err
	}

	lines := []string{}
	if rawLines, ok := wrapper["lines"]; ok && rawLines != nil {
		rawJSON, errMarshal := json.Marshal(rawLines)
		if errMarshal != nil {
			return nil, after, errMarshal
		}
		if errUnmarshal := json.Unmarshal(rawJSON, &lines); errUnmarshal != nil {
			return nil, after, errUnmarshal
		}
	}

	latest := after
	if rawLatest, ok := wrapper["latest-timestamp"]; ok {
		switch value := rawLatest.(type) {
		case float64:
			latest = int64(value)
		case json.Number:
			if parsed, errParse := value.Int64(); errParse == nil {
				latest = parsed
			}
		case int64:
			latest = value
		case int:
			latest = int64(value)
		}
	}
	if latest < after {
		latest = after
	}

	return lines, latest, nil
}

// GetAPIKeys fetches the list of API keys.
// API returns {"api-keys": [...]}.
func (c *Client) GetAPIKeys() ([]string, error) {
	wrapper, err := c.getJSON("/v0/management/api-keys")
	if err != nil {
		return nil, err
	}
	arr, ok := wrapper["api-keys"]
	if !ok {
		return nil, nil
	}
	raw, err := json.Marshal(arr)
	if err != nil {
		return nil, err
	}
	var result []string
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// AddAPIKey adds a new API key by sending old=nil, new=key which appends.
func (c *Client) AddAPIKey(key string) error {
	body := map[string]any{"old": nil, "new": key}
	jsonBody, _ := json.Marshal(body)
	_, err := c.patch("/v0/management/api-keys", strings.NewReader(string(jsonBody)))
	return err
}

// EditAPIKey replaces an API key at the given index.
func (c *Client) EditAPIKey(index int, newValue string) error {
	body := map[string]any{"index": index, "value": newValue}
	jsonBody, _ := json.Marshal(body)
	_, err := c.patch("/v0/management/api-keys", strings.NewReader(string(jsonBody)))
	return err
}

// DeleteAPIKey deletes an API key by index.
func (c *Client) DeleteAPIKey(index int) error {
	_, code, err := c.doRequest("DELETE", fmt.Sprintf("/v0/management/api-keys?index=%d", index), nil)
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("delete failed (HTTP %d)", code)
	}
	return nil
}

// GetGeminiKeys fetches Gemini API keys.
// API returns {"gemini-api-key": [...]}.
func (c *Client) GetGeminiKeys() ([]map[string]any, error) {
	return c.getWrappedKeyList("/v0/management/gemini-api-key", "gemini-api-key")
}

// GetClaudeKeys fetches Claude API keys.
func (c *Client) GetClaudeKeys() ([]map[string]any, error) {
	return c.getWrappedKeyList("/v0/management/claude-api-key", "claude-api-key")
}

// GetCodexKeys fetches Codex API keys.
func (c *Client) GetCodexKeys() ([]map[string]any, error) {
	return c.getWrappedKeyList("/v0/management/codex-api-key", "codex-api-key")
}

// GetVertexKeys fetches Vertex API keys.
func (c *Client) GetVertexKeys() ([]map[string]any, error) {
	return c.getWrappedKeyList("/v0/management/vertex-api-key", "vertex-api-key")
}

// GetOpenAICompat fetches OpenAI compatibility entries.
func (c *Client) GetOpenAICompat() ([]map[string]any, error) {
	return c.getWrappedKeyList("/v0/management/openai-compatibility", "openai-compatibility")
}

// getWrappedKeyList fetches a wrapped list from the API.
func (c *Client) getWrappedKeyList(path, key string) ([]map[string]any, error) {
	wrapper, err := c.getJSON(path)
	if err != nil {
		return nil, err
	}
	return extractList(wrapper, key)
}

// extractList pulls an array of maps from a wrapper object by key.
func extractList(wrapper map[string]any, key string) ([]map[string]any, error) {
	arr, ok := wrapper[key]
	if !ok || arr == nil {
		return nil, nil
	}
	raw, err := json.Marshal(arr)
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) getPlatformAuthFiles() ([]map[string]any, error) {
	const pageSize = 200
	files := make([]map[string]any, 0, pageSize)
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", strconv.Itoa(pageSize))
		wrapper, err := c.getJSON("/v2/credentials?" + query.Encode())
		if err != nil {
			return nil, err
		}
		items, err := extractList(wrapper, "items")
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			files = append(files, normalizePlatformAuthFile(item))
		}
		total := int(getFloat(wrapper, "total"))
		if total > 0 && len(files) >= total {
			break
		}
		if len(items) < pageSize {
			break
		}
	}
	return files, nil
}

func normalizePlatformAuthFile(item map[string]any) map[string]any {
	name := getString(item, "credential_name")
	if name == "" {
		name = getString(item, "runtime_id")
	}
	email := getString(item, "account_email")
	if email == "" {
		email = getString(item, "label")
	}
	selectionKey := getString(item, "selection_key")
	runtimeID := getString(item, "runtime_id")
	return map[string]any{
		"name":            name,
		"channel":         getString(item, "provider"),
		"email":           email,
		"status":          getString(item, "status"),
		"status_message":  getString(item, "status_message"),
		"credential_name": name,
		"selection_key":   selectionKey,
		"disabled":        getBool(item, "disabled"),
		"credential_id":   getString(item, "id"),
		"runtime_id":      runtimeID,
		"updated_at":      getString(item, "updated_at"),
		"last_refresh_at": getString(item, "last_refresh_at"),
		"platform_backed": true,
	}
}

func normalizePlatformCredentialDetail(detail map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range detail {
		result[key] = value
	}
	credentialName := getString(detail, "credential_name")
	if credentialName != "" {
		result["name"] = credentialName
		result["credential_name"] = credentialName
	}
	selectionKey := getString(detail, "selection_key")
	if selectionKey != "" {
		result["selection_key"] = selectionKey
	}
	runtimeID := getString(detail, "runtime_id")
	if runtimeID != "" {
		result["runtime_id"] = runtimeID
	}
	if channel := getString(detail, "provider"); channel != "" {
		result["channel"] = channel
	}
	if email := getString(detail, "account_email"); email != "" {
		result["email"] = email
	} else if label := getString(detail, "label"); label != "" {
		result["email"] = label
	}
	if metadata, ok := detail["metadata"].(map[string]any); ok {
		if prefix := getString(metadata, "prefix"); prefix != "" {
			result["prefix"] = prefix
		}
		if proxyURL := getString(metadata, "proxy_url"); proxyURL != "" {
			result["proxy_url"] = proxyURL
		}
		if attributes, ok := metadata["attributes"].(map[string]any); ok {
			if priority := getString(attributes, "priority"); priority != "" {
				result["priority"] = priority
			}
		}
		if nested, ok := metadata["metadata"].(map[string]any); ok {
			for _, key := range []string{"project_id", "email", "priority"} {
				if value := fmt.Sprintf("%v", nested[key]); value != "" && value != "<nil>" {
					result[key] = value
				}
			}
		}
	}
	result["detail_loaded"] = true
	return result
}

// GetDebug fetches the current debug setting.
func (c *Client) GetDebug() (bool, error) {
	wrapper, err := c.getJSON("/v0/management/debug")
	if err != nil {
		return false, err
	}
	if v, ok := wrapper["debug"]; ok {
		if b, ok := v.(bool); ok {
			return b, nil
		}
	}
	return false, nil
}

// GetAuthStatus polls the OAuth session status.
// Returns status ("wait", "ok", "error") and optional error message.
func (c *Client) GetAuthStatus(state string) (string, string, error) {
	query := url.Values{}
	query.Set("state", state)
	path := "/v0/management/get-auth-status?" + query.Encode()
	wrapper, err := c.getJSON(path)
	if err != nil {
		return "", "", err
	}
	status := getString(wrapper, "status")
	errMsg := getString(wrapper, "error")
	return status, errMsg, nil
}

// ----- Config field update methods -----

// PutBoolField updates a boolean config field.
func (c *Client) PutBoolField(path string, value bool) error {
	body, _ := json.Marshal(map[string]any{"value": value})
	_, err := c.put("/v0/management/"+path, strings.NewReader(string(body)))
	return err
}

// PutIntField updates an integer config field.
func (c *Client) PutIntField(path string, value int) error {
	body, _ := json.Marshal(map[string]any{"value": value})
	_, err := c.put("/v0/management/"+path, strings.NewReader(string(body)))
	return err
}

// PutStringField updates a string config field.
func (c *Client) PutStringField(path string, value string) error {
	body, _ := json.Marshal(map[string]any{"value": value})
	_, err := c.put("/v0/management/"+path, strings.NewReader(string(body)))
	return err
}

// DeleteField sends a DELETE request for a config field.
func (c *Client) DeleteField(path string) error {
	_, _, err := c.doRequest("DELETE", "/v0/management/"+path, nil)
	return err
}
