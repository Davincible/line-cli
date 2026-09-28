package cli

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/kongesque/line-cli/internal/session"
	"github.com/kongesque/line-cli/pkg/line"
)

func (a *App) contactsCommand(args []string) error {
	fs := flag.NewFlagSet("contacts", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	fs.Usage = func() { fmt.Fprintln(a.Err, "Usage: line contacts [options]"); fs.PrintDefaults() }
	var jsonOutput, showIDs bool
	var search string
	limit := 20
	fs.StringVar(&search, "search", "", "find contacts by name")
	fs.IntVar(&limit, "limit", 20, "maximum rows; 0 shows all")
	fs.BoolVar(&showIDs, "show-ids", false, "show full IDs")
	fs.BoolVar(&jsonOutput, "json", false, "write JSON to stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid options; run line contacts --help")
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments; run line contacts --help")
	}
	if limit < 0 {
		return errors.New("limit must be 0 or greater")
	}
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	contacts, err := a.contacts()
	if err != nil {
		return err
	}
	filtered := make([]line.Contact, 0, len(contacts))
	for _, c := range contacts {
		if strings.Contains(strings.ToLower(c.EffectiveDisplayName()), strings.ToLower(search)) {
			filtered = append(filtered, c)
		}
	}
	limitSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "limit" {
			limitSet = true
		}
	})
	if jsonOutput && !limitSet {
		limit = 0
	}
	total := len(filtered)
	if limit > 0 {
		filtered = filtered[:min(limit, total)]
	}
	if jsonOutput {
		return a.json(filtered)
	}
	if total == 0 {
		_, err = fmt.Fprintln(a.Out, "No contacts found. Try a different --search.")
		return err
	}
	fmt.Fprintln(a.Out, "CONTACT")
	for _, c := range filtered {
		name := c.EffectiveDisplayName()
		if name == "" {
			name = c.Mid
		}
		fmt.Fprintln(a.Out, terminalText(name))
		if showIDs && name != c.Mid {
			fmt.Fprintln(a.Out, "  "+terminalText(c.Mid))
		}
	}
	_, err = fmt.Fprintf(a.Out, "\nShowing %d of %d contacts. Find someone: line contacts --search NAME\n", len(filtered), total)
	return err
}

func (a *App) contacts() ([]line.Contact, error) {
	var ids []string
	if err := a.Manager.Do(func(api session.API) (err error) { ids, err = api.GetAllContactIds(); return }); err != nil {
		return nil, err
	}
	result := make([]line.Contact, 0, len(ids))
	seen := make(map[string]bool)
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		var response *line.ContactsResponse
		if err := a.Manager.Do(func(api session.API) (err error) { response, err = api.GetContactsV2(ids[start:end]); return }); err != nil {
			return nil, err
		}
		if response == nil {
			return nil, errors.New("LINE returned an empty contacts response")
		}
		for mid, wrapper := range response.Contacts {
			contact := wrapper.Contact
			if contact.Mid == "" {
				contact.Mid = mid
			}
			if !seen[contact.Mid] {
				result = append(result, contact)
				seen[contact.Mid] = true
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i].EffectiveDisplayName(), result[j].EffectiveDisplayName()
		if a == b {
			return result[i].Mid < result[j].Mid
		}
		return a < b
	})
	return result, nil
}
