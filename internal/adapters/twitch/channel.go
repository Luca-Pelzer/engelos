package twitch

import (
	"context"
	"fmt"
	"strings"

	"github.com/nicklaw5/helix/v2"
)

// categorySearchLimit bounds the SearchCategories page the adapter scans for an
// exact name match. Twitch ranks the closest matches first, so the canonical
// title (e.g. "Just Chatting") is reliably within the first page.
const categorySearchLimit = 100

// CategoryView is a neutral view of a Twitch category (game), free of any helix
// type so callers never depend on the SDK.
type CategoryView struct {
	ID   string
	Name string
}

// SetStreamTitle updates the channel's stream title. Returns
// [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) SetStreamTitle(ctx context.Context, login, title string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("twitch: set stream title: title is empty")
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return err
	}
	resp, err := hx.EditChannelInformation(&helix.EditChannelInformationParams{
		BroadcasterID: bid,
		Title:         title,
	})
	if err != nil {
		return fmt.Errorf("twitch: set stream title on %q: %w", login, err)
	}
	return helixStatusError("set stream title", resp.StatusCode, resp.ErrorMessage)
}

// SetCategory sets the channel's category (game) by name. The name is resolved
// to a Twitch category id via SearchCategories, requiring an exact
// (case-insensitive) name match; an ambiguous or unknown name is an error so a
// rule never silently sets the wrong game. Returns the resolved [CategoryView].
// Returns [ErrHelixUnavailable] in anonymous mode.
func (a *Adapter) SetCategory(ctx context.Context, login, name string) (CategoryView, error) {
	if err := ctx.Err(); err != nil {
		return CategoryView{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return CategoryView{}, fmt.Errorf("twitch: set category: name is empty")
	}
	hx, err := a.helixClientOrErr()
	if err != nil {
		return CategoryView{}, err
	}
	bid, err := a.rewardBroadcasterID(ctx, login)
	if err != nil {
		return CategoryView{}, err
	}

	cat, err := a.resolveCategory(hx, name)
	if err != nil {
		return CategoryView{}, err
	}

	resp, err := hx.EditChannelInformation(&helix.EditChannelInformationParams{
		BroadcasterID: bid,
		GameID:        cat.ID,
	})
	if err != nil {
		return CategoryView{}, fmt.Errorf("twitch: set category on %q: %w", login, err)
	}
	if err := helixStatusError("set category", resp.StatusCode, resp.ErrorMessage); err != nil {
		return CategoryView{}, err
	}
	return cat, nil
}

// resolveCategory searches Twitch categories for name and returns the one whose
// name matches exactly (case-insensitively), erroring when none does.
func (a *Adapter) resolveCategory(hx helixClient, name string) (CategoryView, error) {
	resp, err := hx.SearchCategories(&helix.SearchCategoriesParams{
		Query: name,
		First: categorySearchLimit,
	})
	if err != nil {
		return CategoryView{}, fmt.Errorf("twitch: search category %q: %w", name, err)
	}
	if err := helixStatusError("search category", resp.StatusCode, resp.ErrorMessage); err != nil {
		return CategoryView{}, err
	}
	for _, c := range resp.Data.Categories {
		if strings.EqualFold(strings.TrimSpace(c.Name), name) && c.ID != "" {
			return CategoryView{ID: c.ID, Name: c.Name}, nil
		}
	}
	return CategoryView{}, fmt.Errorf("twitch: set category: no category exactly named %q", name)
}
