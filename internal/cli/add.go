package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"unicode"

	"github.com/kongesque/line-cli/internal/session"
	"github.com/kongesque/line-cli/pkg/line"
)

// foundContact is one result of a search by LINE ID, plus whether the account
// already has that person or official account as a friend.
type foundContact struct {
	SearchID      string `json:"search_id"`
	Mid           string `json:"mid"`
	DisplayName   string `json:"display_name"`
	StatusMessage string `json:"status_message,omitempty"`
	Friend        bool   `json:"friend"`
}

type addFriendResult struct {
	foundContact
	Action          string `json:"action"`
	Status          string `json:"status"`
	RequestSequence int64  `json:"request_sequence,omitempty"`
}

// validateSearchID rejects input LINE Chrome's search box would never send:
// empty text, whitespace, control characters, or more than 64 characters.
func validateSearchID(id string) error {
	if id == "" {
		return errors.New("provide the LINE ID to search for, for example someone or @shopname")
	}
	if len([]rune(id)) > 64 {
		return errors.New("LINE ID is too long; IDs are at most 64 characters")
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("LINE ID cannot contain spaces or control characters")
		}
	}
	return nil
}

// findByID looks an ID up and reports whether it is already a friend. The
// caller holds the process lock.
func (a *App) findByID(id string) (*foundContact, error) {
	var contact *line.Contact
	if err := a.Manager.Do(func(api session.API) (err error) { contact, err = api.FindContactByUserid(id); return }); err != nil {
		return nil, fmt.Errorf("no LINE account found for ID %q, or LINE refused the search: %w", id, err)
	}
	if contact == nil || !fullUserMID.MatchString(contact.Mid) {
		return nil, fmt.Errorf("no LINE account found for ID %q", id)
	}
	var ids []string
	if err := a.Manager.Do(func(api session.API) (err error) { ids, err = api.GetAllContactIds(); return }); err != nil {
		return nil, err
	}
	friend := false
	for _, mid := range ids {
		if mid == contact.Mid {
			friend = true
			break
		}
	}
	return &foundContact{
		SearchID:      id,
		Mid:           contact.Mid,
		DisplayName:   contact.EffectiveDisplayName(),
		StatusMessage: contact.StatusMessage,
		Friend:        friend,
	}, nil
}

func (a *App) printFound(found *foundContact) error {
	name := terminalText(found.DisplayName)
	if strings.TrimSpace(name) == "" {
		name = "(name unavailable)"
	}
	friend := "no"
	if found.Friend {
		friend = "yes"
	}
	_, err := fmt.Fprintf(a.Out, "NAME     %s\nUSER ID  %s\nFRIEND   %s\n", name, found.Mid, friend)
	if err == nil && found.StatusMessage != "" {
		_, err = fmt.Fprintf(a.Out, "STATUS   %s\n", terminalText(found.StatusMessage))
	}
	return err
}

// addCommand adds a user or official account found by LINE ID. It is a remote
// mutation: attempted once with a reserved request sequence, never retried.
func (a *App) addCommand(args []string) error {
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	fs.Usage = func() {
		fmt.Fprintln(a.Err, "Usage: line add LINE_ID [--json]\nSearches LINE for the ID (someone, or @shopname for an official account) and adds it as a friend.\nAn ID that is already a friend is reported and left alone. Preview first with line contacts --find LINE_ID.")
		fs.PrintDefaults()
	}
	jsonOutput := fs.Bool("json", false, "write JSON to stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid options; run line add --help")
	}
	if id == "" && fs.NArg() == 1 {
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return errors.New("unexpected arguments; run line add LINE_ID")
	}
	if err := validateSearchID(id); err != nil {
		return err
	}
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	found, err := a.findByID(id)
	if err != nil {
		return err
	}
	result := addFriendResult{foundContact: *found, Action: "add_friend", Status: "already_friend"}
	if !found.Friend {
		seq, err := a.Manager.ReserveSequence()
		if err != nil {
			return err
		}
		if err := a.Manager.Mutate(func(api session.API) error { return api.AddFriendByMid(seq, found.Mid, id) }); err != nil {
			return fmt.Errorf("add_friend did not return success; check LINE before retrying: %w", err)
		}
		result.Status, result.RequestSequence, result.Friend = "added", seq, true
	}
	if *jsonOutput {
		return a.json(result)
	}
	if err := a.printFound(&result.foundContact); err != nil {
		return err
	}
	message := "\nAlready a friend; nothing was changed."
	if result.Status == "added" {
		message = "\nAdded as a friend. Message them with: line send " + found.Mid + " --text ..."
	}
	_, err = fmt.Fprintln(a.Out, message)
	return err
}
