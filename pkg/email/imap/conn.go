package imap

import (
	imapv2 "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Conn is the subset of imapclient.Client methods used by this package.
// Extracting it as an interface lets tests inject a mock without dialing a
// real IMAP server.
type Conn interface {
	// ListMailboxes returns the names of all selectable mailboxes.
	ListMailboxes() ([]string, error)
	// Select selects the named mailbox in read-only mode.
	Select(mailbox string) error
	// UIDSearch executes a UID SEARCH and returns the matching UIDs.
	UIDSearch(criteria *imapv2.SearchCriteria) ([]imapv2.UID, error)
	// FetchMessages fetches the requested body sections for the given UID set.
	FetchMessages(numSet imapv2.NumSet, options *imapv2.FetchOptions) ([]*imapclient.FetchMessageBuffer, error)
	// Logout sends a LOGOUT command and waits for the server response.
	Logout() error
	// Close closes the underlying network connection.
	Close() error
}

// realConn wraps *imapclient.Client and implements Conn by delegating to the
// go-imap command objects and waiting for their responses inline.
type realConn struct {
	c *imapclient.Client
}

func (r *realConn) ListMailboxes() ([]string, error) {
	entries, err := r.c.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		noselect := false
		for _, a := range e.Attrs {
			if a == imapv2.MailboxAttrNoSelect {
				noselect = true
				break
			}
		}
		if !noselect {
			names = append(names, e.Mailbox)
		}
	}
	return names, nil
}

func (r *realConn) Select(mailbox string) error {
	_, err := r.c.Select(mailbox, &imapv2.SelectOptions{ReadOnly: true}).Wait()
	return err
}

func (r *realConn) UIDSearch(criteria *imapv2.SearchCriteria) ([]imapv2.UID, error) {
	data, err := r.c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, err
	}
	return data.AllUIDs(), nil
}

func (r *realConn) FetchMessages(numSet imapv2.NumSet, options *imapv2.FetchOptions) ([]*imapclient.FetchMessageBuffer, error) {
	return r.c.Fetch(numSet, options).Collect()
}

func (r *realConn) Logout() error {
	return r.c.Logout().Wait()
}

func (r *realConn) Close() error {
	return r.c.Close()
}
