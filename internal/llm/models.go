package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/L-K-M/Earnest/internal/outbound"
)

// DiscoveredModel is a catalogue suggestion, not a configured model. Missing
// capability and price fields mean unknown, never false or free.
type DiscoveredModel struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Chat           *bool   `json:"chat,omitempty"`
	Embedding      *bool   `json:"embedding,omitempty"`
	InputPer1M     *string `json:"input_per_1m,omitempty"`
	OutputPer1M    *string `json:"output_per_1m,omitempty"`
	EmbeddingPer1M *string `json:"embedding_per_1m,omitempty"`
	Dimensions     *int    `json:"dimensions,omitempty"`
	Truncatable    *bool   `json:"truncatable,omitempty"`
}

const maxCataloguePages = 100
const maxCatalogueModels = 10000

// DiscoverModels reads a catalogue using the same egress and authentication
// rules as inference. It never sends prompts or changes the running router.
func DiscoverModels(ctx context.Context, name string, cfg ProviderConfig, client *outbound.Client) ([]DiscoveredModel, error) {
	if client == nil {
		return nil, errors.New("model discovery is unavailable")
	}
	provider, err := newProvider(name, cfg, client)
	if err != nil {
		return nil, errors.New("invalid provider configuration")
	}
	var endpoint string
	var headers http.Header
	var ports []uint16
	var timeout time.Duration
	switch p := provider.(type) {
	case *OpenAI:
		endpoint, headers, ports, timeout = p.baseURL+"/models", p.authHeader(), p.extraPorts, p.timeout
	case *Anthropic:
		endpoint, headers, ports, timeout = p.baseURL+"/v1/models", p.authHeader(), p.extraPorts, p.timeout
	case *Gemini:
		endpoint, headers, ports, timeout = p.baseURL+"/models", p.authHeader(), p.extraPorts, p.timeout
	default:
		return nil, errors.New("this provider does not support model discovery; add models manually")
	}
	// Bound the whole catalogue, including pagination and host pacing.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("invalid provider URL")
	}
	venice := cfg.Kind == KindOpenAI && strings.EqualFold(u.Hostname(), "api.venice.ai")
	query := u.Query()
	if venice {
		query.Set("type", "all")
	}
	models := make([]DiscoveredModel, 0)
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	total := 0
	for page := 0; page < maxCataloguePages; page++ {
		u.RawQuery = query.Encode()
		raw, err := catalogueGET(ctx, client, u.String(), headers, timeout, ports)
		if err != nil {
			return nil, err
		}
		entries, cursor, err := decodeCatalogue(raw, cfg.Kind, venice)
		if err != nil {
			return nil, err
		}
		total += len(entries)
		if total > maxCatalogueModels {
			return nil, errors.New("provider catalogue is too large; add models manually")
		}
		for _, model := range entries {
			if !seenIDs[model.ID] {
				models = append(models, model)
				seenIDs[model.ID] = true
			}
		}
		if cursor == "" {
			slices.SortFunc(models, func(a, b DiscoveredModel) int { return strings.Compare(a.ID, b.ID) })
			return models, nil
		}
		if len(cursor) > 4096 || seenCursors[cursor] {
			return nil, errors.New("provider returned invalid catalogue pagination")
		}
		seenCursors[cursor] = true
		switch cfg.Kind {
		case KindGemini:
			query.Set("pageToken", cursor)
		case KindAnthropic:
			query.Set("after_id", cursor)
		default:
			query.Set("after", cursor)
		}
	}
	return nil, errors.New("provider catalogue has too many pages; add models manually")
}

func catalogueGET(ctx context.Context, client *outbound.Client, endpoint string, headers http.Header, timeout time.Duration, ports []uint16) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid provider URL")
	}
	req.Header = headers.Clone()
	req.Header.Set("Accept", "application/json")
	result, err := client.Do(req, outbound.Policy{Purpose: outbound.LLM, Timeout: timeout, Redirect: outbound.RedirectNone, ExtraPorts: ports})
	if result != nil && result.Response != nil {
		defer result.Response.Body.Close()
	}
	if err != nil {
		// Transport errors can embed URLs and upstream response content. Only
		// expose our classification, including for denied private destinations.
		return nil, fmt.Errorf("could not fetch models (%s); check the provider URL, network and outbound allowlist", outbound.ErrorClass(err))
	}
	if result == nil || result.Response == nil {
		return nil, errors.New("provider returned no model catalogue")
	}
	switch status := result.Response.StatusCode; {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, errors.New("provider rejected access; check the API key and its permissions")
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		return nil, errors.New("provider does not expose model discovery at this URL; add models manually")
	case status == http.StatusTooManyRequests:
		return nil, errors.New("provider rate limit reached; retry fetching models later")
	case status < 200 || status >= 300:
		return nil, fmt.Errorf("provider model discovery returned HTTP %d; retry or add models manually", status)
	}
	raw, err := io.ReadAll(result.Response.Body)
	if err != nil {
		return nil, errors.New("could not read provider model catalogue")
	}
	return raw, nil
}

type catalogueEntry struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	DisplayName   string   `json:"displayName"`
	AnthropicName string   `json:"display_name"`
	Type          string   `json:"type"`
	Methods       []string `json:"supportedGenerationMethods"`
	Spec          struct {
		Name    string `json:"name"`
		Pricing struct {
			Input struct {
				USD json.Number `json:"usd"`
			} `json:"input"`
			Output struct {
				USD json.Number `json:"usd"`
			} `json:"output"`
		} `json:"pricing"`
	} `json:"model_spec"`
}

func decodeCatalogue(raw []byte, kind ProviderKind, venice bool) ([]DiscoveredModel, string, error) {
	var response struct {
		Data          []catalogueEntry `json:"data"`
		Models        []catalogueEntry `json:"models"`
		HasMore       bool             `json:"has_more"`
		LastID        string           `json:"last_id"`
		NextPageToken string           `json:"nextPageToken"`
	}
	invalid := errors.New("provider returned an invalid model catalogue; add models manually")
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, "", invalid
	}
	entries, cursor := response.Data, ""
	if kind == KindGemini {
		entries, cursor = response.Models, response.NextPageToken
	} else if response.HasMore {
		cursor = response.LastID
		if cursor == "" {
			return nil, "", invalid
		}
	}
	// An explicit empty list is valid. Missing/null lists usually mean the
	// URL points at a different API; do not report them as successful discovery.
	if entries == nil || len(entries) > maxCatalogueModels {
		return nil, "", invalid
	}
	result := make([]DiscoveredModel, 0, len(entries))
	for _, entry := range entries {
		model := DiscoveredModel{ID: entry.ID, Name: entry.AnthropicName}
		if kind == KindGemini {
			model.ID, model.Name = strings.TrimPrefix(entry.Name, "models/"), entry.DisplayName
		}
		if model.ID == "" || len(model.ID) > 256 || strings.ContainsFunc(model.ID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return nil, "", invalid
		}
		switch {
		case kind == KindAnthropic:
			model.Chat, model.Embedding = boolPointer(true), boolPointer(false)
		case kind == KindGemini:
			model.Chat = boolPointer(slices.Contains(entry.Methods, "generateContent"))
			model.Embedding = boolPointer(slices.Contains(entry.Methods, "embedContent"))
			if !*model.Chat && !*model.Embedding {
				continue
			}
		case venice:
			if entry.Type != "text" && entry.Type != "embedding" {
				continue
			}
			model.Name = entry.Spec.Name
			model.Chat, model.Embedding = boolPointer(entry.Type == "text"), boolPointer(entry.Type == "embedding")
			// Venice publishes these rates in USD per million tokens. Other
			// compatible APIs have no common price schema, so do not guess units.
			if *model.Chat {
				model.InputPer1M, model.OutputPer1M = cataloguePrice(entry.Spec.Pricing.Input.USD), cataloguePrice(entry.Spec.Pricing.Output.USD)
			} else {
				model.EmbeddingPer1M = cataloguePrice(entry.Spec.Pricing.Input.USD)
			}
		}
		if model.Name == "" || len(model.Name) > 256 || strings.ContainsFunc(model.Name, unicode.IsControl) {
			model.Name = model.ID
		}
		result = append(result, model)
	}
	return result, cursor, nil
}

func boolPointer(value bool) *bool { return &value }

func cataloguePrice(number json.Number) *string {
	value, err := ParseUSD(number.String())
	if err != nil || value > 99999999999999 {
		return nil
	}
	text := value.String()
	return &text
}
