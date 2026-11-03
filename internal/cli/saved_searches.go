package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type savedSearchBody struct {
	Name    string       `json:"name"`
	Query   string       `json:"query"`
	Filters searchFilter `json:"filters"`
}

type savedSearchRename struct {
	Name string `json:"name"`
}

// savedSearches exposes the named search views the API stores. Creating,
// renaming and removing them needs feeds:write; listing and opening them only
// needs entries:read.
func (a *app) savedSearches(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usagef("usage: earnest saved-searches list|add <name> <query>|show <id>|rename <id> <name>|remove <id>")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return a.savedSearchesList(ctx, c, args[1:])
	case "add":
		return a.savedSearchAdd(ctx, c, args[1:])
	case "show":
		return a.savedSearchShow(ctx, c, args[1:])
	case "rename":
		return a.savedSearchRename(ctx, c, args[1:])
	case "remove":
		return a.savedSearchRemove(ctx, c, args[1:])
	default:
		return usagef("unknown saved-searches command %q", args[0])
	}
}

func (a *app) savedSearchesList(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("saved-searches list")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest saved-searches list")
	}
	raw, err := c.get(ctx, "/v1/saved-searches", nil)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var searches []savedSearch
	if err := decode(raw, &searches); err != nil {
		return err
	}
	rows := tableRows(len(searches))
	for _, search := range searches {
		rows = append(rows, []string{
			strconv.FormatInt(search.ID, 10),
			cell(search.Name),
			cell(search.Query),
			searchFilterCell(search.Filters),
		})
	}
	writeTable(a.out, []string{"ID", "NAME", "QUERY", "FILTERS"}, rows)
	return nil
}

func (a *app) savedSearchAdd(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("saved-searches add")
	feed := fs.Int64("feed", 0, "subscription id")
	folder := fs.Int64("folder", 0, "folder id")
	limit := fs.Int("limit", 0, "maximum hits when opened")
	if err := parseFlags(fs, args, map[string]bool{"feed": true, "folder": true, "limit": true}); err != nil || fs.NArg() != 2 {
		return usagef("usage: earnest saved-searches add <name> <query> [--feed id] [--folder id] [--limit n]")
	}
	if *feed < 0 || *folder < 0 {
		return usagef("--feed and --folder must be positive")
	}
	if *limit < 0 || *limit > maxEntryLimit {
		return usagef("--limit must be between 1 and %d", maxEntryLimit)
	}
	name, query := fs.Arg(0), strings.TrimSpace(fs.Arg(1))
	if name == "" || query == "" {
		return usagef("name and query are required")
	}
	raw, err := c.send(ctx, http.MethodPost, "/v1/saved-searches", nil, savedSearchBody{Name: name, Query: query, Filters: searchFilter{SubscriptionID: *feed, FolderID: *folder, Limit: *limit}})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	var result savedSearch
	if err := decode(raw, &result); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "saved search %q recorded (id %d)\n", result.Name, result.ID)
	return nil
}

func (a *app) savedSearchShow(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("saved-searches show")
	cursor := fs.String("cursor", "", "exclusive cursor from the previous page")
	if err := parseFlags(fs, args, map[string]bool{"cursor": true}); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest saved-searches show <id> [--cursor c]")
	}
	id, err := parseID(fs.Arg(0))
	if err != nil {
		return err
	}
	raw, err := c.get(ctx, "/v1/saved-searches/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return err
	}
	var search savedSearch
	if err := decode(raw, &search); err != nil {
		return err
	}
	values := url.Values{}
	if *cursor != "" {
		values.Set("cursor", *cursor)
	}
	raw, err = c.get(ctx, "/v1/saved-searches/"+strconv.FormatInt(id, 10)+"/entries", values)
	if err != nil {
		return err
	}
	var page searchPage
	if err := decode(raw, &page); err != nil {
		return err
	}
	if a.json {
		return a.encodeJSON(struct {
			Search     savedSearch   `json:"search"`
			Matches    []searchMatch `json:"matches"`
			NextCursor string        `json:"next_cursor,omitempty"`
		}{Search: search, Matches: page.Matches, NextCursor: page.NextCursor})
	}
	fmt.Fprintf(a.out, "%s (id %d): %s\n", search.Name, search.ID, search.Query)
	if filter := searchFilterCell(search.Filters); filter != "" {
		fmt.Fprintf(a.out, "filters: %s\n", filter)
	}
	a.printSearchPage(page)
	return nil
}

func (a *app) savedSearchRename(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("saved-searches rename")
	if err := fs.Parse(args); err != nil || fs.NArg() != 2 {
		return usagef("usage: earnest saved-searches rename <id> <name>")
	}
	id, err := parseID(fs.Arg(0))
	if err != nil {
		return err
	}
	name := fs.Arg(1)
	if name == "" {
		return usagef("name is required")
	}
	raw, err := c.send(ctx, http.MethodPatch, fmt.Sprintf("/v1/saved-searches/%d", id), nil, savedSearchRename{Name: name})
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	fmt.Fprintf(a.out, "renamed saved search %d to %q\n", id, name)
	return nil
}

func (a *app) savedSearchRemove(ctx context.Context, c *client, args []string) error {
	fs := newFlagSet("saved-searches remove")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return usagef("usage: earnest saved-searches remove <id>")
	}
	id, err := parseID(fs.Arg(0))
	if err != nil {
		return err
	}
	raw, err := c.send(ctx, http.MethodDelete, fmt.Sprintf("/v1/saved-searches/%d", id), nil, nil)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(raw)
	}
	fmt.Fprintf(a.out, "removed saved search %d\n", id)
	return nil
}

// searchFilterCell renders a saved search's filters for the table.
func searchFilterCell(filter searchFilter) string {
	parts := make([]string, 0, 3)
	if filter.SubscriptionID > 0 {
		parts = append(parts, "feed "+strconv.FormatInt(filter.SubscriptionID, 10))
	}
	if filter.FolderID > 0 {
		parts = append(parts, "folder "+strconv.FormatInt(filter.FolderID, 10))
	}
	if filter.Limit > 0 {
		parts = append(parts, "limit "+strconv.Itoa(filter.Limit))
	}
	return strings.Join(parts, ",")
}
