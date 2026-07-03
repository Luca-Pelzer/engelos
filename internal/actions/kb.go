package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KBHit is one knowledge-base entry returned by a KBSearcher, reduced to the
// fields the kb:lookup node renders into its context output.
type KBHit struct {
	Category string
	Title    string
	Content  string
}

// KBSearcher is the read surface the kb:lookup action needs. The kb store's
// adapter satisfies it; a nil value disables the node with an empty result (as
// if the channel had no matching entries) rather than aborting the rule.
type KBSearcher interface {
	Search(ctx context.Context, tenantID, channel, query, category string, limit int) ([]KBHit, error)
}

const (
	kbDefaultLimit    = 3
	kbMaxLimit        = 10
	kbMaxContextBytes = 6144
)

type kbLookupConfig struct {
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
	Category string `json:"category"`
}

type kbLookupAction struct {
	kb KBSearcher
}

func (kbLookupAction) Definition() PluginDefinition {
	return PluginDefinition{
		ID:          "kb:lookup",
		Name:        "Knowledge base lookup",
		Description: "Searches the channel knowledge base and renders the top entries for a later node. Query supports $(...) and defaults to the triggering message; an optional category narrows the search. Outputs kb.hits, kb.context and kb.top_title.",
	}
}

func (a kbLookupAction) Execute(ec *ExecutionContext, config json.RawMessage) (*ActionResult, error) {
	var c kbLookupConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("actions: kb:lookup config: %w", err)
	}
	query := strings.TrimSpace(c.Query)
	if query == "" {
		query = strings.TrimSpace(ec.Trigger.Text)
	}

	limit := c.Limit
	if limit <= 0 {
		limit = kbDefaultLimit
	}
	if limit > kbMaxLimit {
		limit = kbMaxLimit
	}

	empty := map[string]any{"kb.hits": 0, "kb.context": "", "kb.top_title": ""}
	if a.kb == nil || query == "" {
		return &ActionResult{Outputs: empty}, nil
	}

	tenant := ec.TenantID
	channel := ec.Channel
	if channel == "" {
		channel = ec.Trigger.Channel
	}
	hits, err := a.kb.Search(ec.Ctx, tenant, channel, query, strings.TrimSpace(c.Category), limit)
	if err != nil {
		return nil, fmt.Errorf("actions: kb:lookup: %w", err)
	}
	if len(hits) == 0 {
		return &ActionResult{Outputs: empty}, nil
	}

	return &ActionResult{Outputs: map[string]any{
		"kb.hits":      len(hits),
		"kb.context":   renderKBContext(hits),
		"kb.top_title": hits[0].Title,
	}}, nil
}

// renderKBContext joins the hits into a compact block a prompt or chat reply can
// embed: one "[category] title: content" line per entry, newest match first,
// truncated at kbMaxContextBytes so a few large entries can never blow up a
// downstream AI prompt. Truncation happens on a line boundary so a rendered
// entry is never cut mid-sentence.
func renderKBContext(hits []KBHit) string {
	var b strings.Builder
	for _, h := range hits {
		line := fmt.Sprintf("[%s] %s: %s", h.Category, h.Title, h.Content)
		if b.Len()+len(line)+1 > kbMaxContextBytes {
			break
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}
