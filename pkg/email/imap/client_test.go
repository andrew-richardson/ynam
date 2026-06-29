package imap

import (
	"context"
	"errors"
	"testing"
	"time"

	"ynam/pkg/config"
	"ynam/pkg/email/imap/mocks"

	imapv2 "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	_ "ynam/pkg/email/amazon"
	_ "ynam/pkg/email/apple"
	_ "ynam/pkg/email/paypal"
	_ "ynam/pkg/email/venmo"
)

// testAccount is a minimal single-mailbox EmailAccount used across tests.
var testAccount = config.EmailAccount{
	Email:      "user@example.com",
	Password:   "pass",
	IMAPServer: "imap.example.com",
	Mailboxes:  []string{"INBOX"},
}

// accountWithMailboxes returns a copy of testAccount with the given mailboxes.
func accountWithMailboxes(boxes ...string) config.EmailAccount {
	a := testAccount
	a.Mailboxes = boxes
	return a
}

func amazonFilters() map[string]config.ServiceConfig {
	return map[string]config.ServiceConfig{
		"amazon": {
			Sync:    true,
			From:    []string{"auto-confirm@amazon.com"},
			Subject: []string{"Ordered"},
		},
	}
}

// TestFetchSince_SelectSkipped verifies that a mailbox which cannot be selected
// is skipped while other mailboxes are still searched (no error surfaced).
func TestFetchSince_SelectSkipped(t *testing.T) {
	conn := mocks.NewMockConn(t)
	conn.EXPECT().Select("Junk").Return(errors.New("no such mailbox"))
	conn.EXPECT().Select("INBOX").Return(nil)
	conn.EXPECT().UIDSearch(mock.Anything).Return([]imapv2.UID{}, nil)
	conn.EXPECT().Logout().Return(nil)
	conn.EXPECT().Close().Return(nil)

	client := NewClientWithConn(accountWithMailboxes("Junk", "INBOX"), conn)
	defer client.Close()

	txns, count, err := client.FetchSince(time.Now().Add(-24*time.Hour), time.Time{}, []string{"amazon"}, amazonFilters())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, txns)
}

// TestFetchSince_AllMailboxesFailErrors verifies that if every mailbox fails,
// the error is surfaced rather than silently returning empty.
func TestFetchSince_AllMailboxesFailErrors(t *testing.T) {
	conn := mocks.NewMockConn(t)
	conn.EXPECT().Select("INBOX").Return(errors.New("no such mailbox"))
	conn.EXPECT().Logout().Return(nil)
	conn.EXPECT().Close().Return(nil)

	client := NewClientWithConn(testAccount, conn)
	defer client.Close()

	_, _, err := client.FetchSince(time.Now().Add(-24*time.Hour), time.Time{}, []string{"amazon"}, amazonFilters())
	require.Error(t, err)
}

// TestFetchSince_SearchError verifies that a UIDSearch failure on the only
// mailbox is surfaced.
func TestFetchSince_SearchError(t *testing.T) {
	conn := mocks.NewMockConn(t)
	conn.EXPECT().Select("INBOX").Return(nil)
	conn.EXPECT().UIDSearch(mock.Anything).Return(nil, errors.New("search failed"))
	conn.EXPECT().Logout().Return(nil)
	conn.EXPECT().Close().Return(nil)

	client := NewClientWithConn(testAccount, conn)
	defer client.Close()

	_, _, err := client.FetchSince(time.Now().Add(-24*time.Hour), time.Time{}, []string{"amazon"}, amazonFilters())
	require.Error(t, err)
}

// TestFetchSince_Empty verifies that zero UIDs returns empty results with no error.
func TestFetchSince_Empty(t *testing.T) {
	conn := mocks.NewMockConn(t)
	conn.EXPECT().Select("INBOX").Return(nil)
	conn.EXPECT().UIDSearch(mock.Anything).Return([]imapv2.UID{}, nil)
	conn.EXPECT().Logout().Return(nil)
	conn.EXPECT().Close().Return(nil)

	client := NewClientWithConn(testAccount, conn)
	defer client.Close()

	txns, count, err := client.FetchSince(time.Now().Add(-24*time.Hour), time.Time{}, []string{"amazon"}, amazonFilters())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, txns)
}

// TestFetchSince_ParsesMessages verifies the full path: UIDs → fetch → parse.
func TestFetchSince_ParsesMessages(t *testing.T) {
	msg := &imapclient.FetchMessageBuffer{
		Envelope: &imapv2.Envelope{
			Subject:   `Your Amazon.com order of "Test Item"`,
			From:      []imapv2.Address{{Mailbox: "auto-confirm", Host: "amazon.com"}},
			MessageID: "<msg-1@amazon.com>",
		},
		BodySection: []imapclient.FetchBodySectionBuffer{
			{Bytes: []byte(buildAmazonEML())},
		},
	}

	conn := mocks.NewMockConn(t)
	conn.EXPECT().Select("INBOX").Return(nil)
	conn.EXPECT().UIDSearch(mock.Anything).Return([]imapv2.UID{1}, nil).Once()
	conn.EXPECT().FetchMessages(mock.Anything, mock.Anything).Return([]*imapclient.FetchMessageBuffer{msg}, nil)
	conn.EXPECT().Logout().Return(nil)
	conn.EXPECT().Close().Return(nil)

	client := NewClientWithConn(testAccount, conn)
	defer client.Close()

	txns, count, err := client.FetchSince(time.Now().Add(-24*time.Hour), time.Time{}, []string{"amazon"}, amazonFilters())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.NotEmpty(t, txns)
}

// TestFetchSince_MultipleMailboxes verifies that every configured mailbox is
// searched and that a message appearing in more than one is de-duplicated by
// Message-ID.
func TestFetchSince_MultipleMailboxes(t *testing.T) {
	msg := &imapclient.FetchMessageBuffer{
		Envelope: &imapv2.Envelope{
			Subject:   `Your Amazon.com order of "Test Item"`,
			From:      []imapv2.Address{{Mailbox: "auto-confirm", Host: "amazon.com"}},
			MessageID: "<dupe@amazon.com>",
		},
		BodySection: []imapclient.FetchBodySectionBuffer{
			{Bytes: []byte(buildAmazonEML())},
		},
	}

	conn := mocks.NewMockConn(t)
	conn.EXPECT().Select("INBOX").Return(nil)
	conn.EXPECT().Select("Confirmations & Receipts").Return(nil)
	conn.EXPECT().UIDSearch(mock.Anything).Return([]imapv2.UID{1}, nil).Times(2)
	conn.EXPECT().FetchMessages(mock.Anything, mock.Anything).Return([]*imapclient.FetchMessageBuffer{msg}, nil).Times(2)
	conn.EXPECT().Logout().Return(nil)
	conn.EXPECT().Close().Return(nil)

	client := NewClientWithConn(accountWithMailboxes("INBOX", "Confirmations & Receipts"), conn)
	defer client.Close()

	txns, count, err := client.FetchSince(time.Now().Add(-24*time.Hour), time.Time{}, []string{"amazon"}, amazonFilters())
	require.NoError(t, err)
	assert.Equal(t, 1, count, "duplicate message across mailboxes should be counted once")
	assert.NotEmpty(t, txns)
}

// TestFetchSince_FetchError verifies that a FetchMessages failure on the only
// mailbox is surfaced.
func TestFetchSince_FetchError(t *testing.T) {
	conn := mocks.NewMockConn(t)
	conn.EXPECT().Select("INBOX").Return(nil)
	conn.EXPECT().UIDSearch(mock.Anything).Return([]imapv2.UID{42}, nil)
	conn.EXPECT().FetchMessages(mock.Anything, mock.Anything).Return(nil, errors.New("fetch failed"))
	conn.EXPECT().Logout().Return(nil)
	conn.EXPECT().Close().Return(nil)

	client := NewClientWithConn(testAccount, conn)
	defer client.Close()

	_, _, err := client.FetchSince(time.Now().Add(-24*time.Hour), time.Time{}, []string{"amazon"}, amazonFilters())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch")
}

// TestFetchAll_WithFactory exercises fetchAll with an injected factory.
func TestFetchAll_WithFactory(t *testing.T) {
	conn := mocks.NewMockConn(t)
	conn.EXPECT().Select("INBOX").Return(nil)
	conn.EXPECT().UIDSearch(mock.Anything).Return([]imapv2.UID{}, nil)
	// Logout and Close are called from defer client.Close(); the ctx-done
	// goroutine may also call Close a second time depending on scheduling.
	conn.EXPECT().Logout().Return(nil).Maybe()
	conn.EXPECT().Close().Return(nil).Maybe()

	cfg := &config.Config{
		YNABAPIToken: "token",
		YNABBudgetID: "budget",
		DaysSince:    7,
		Services: map[string]config.ServiceConfig{
			"amazon": {Sync: true, From: []string{"auto-confirm@amazon.com"}, Subject: []string{"Ordered"}},
		},
		EmailAccounts: []config.EmailAccount{testAccount},
	}

	factory := func(account config.EmailAccount) (*Client, error) {
		return NewClientWithConn(account, conn), nil
	}

	results := fetchAll(context.Background(), cfg, 7, nil, factory)
	require.Len(t, results, 1)
	assert.NoError(t, results[0].Err)
	assert.Equal(t, testAccount.Email, results[0].Account)
}

// TestFetchAll_FactoryError verifies that a dial failure produces an error result.
func TestFetchAll_FactoryError(t *testing.T) {
	cfg := &config.Config{
		YNABAPIToken: "token",
		YNABBudgetID: "budget",
		DaysSince:    7,
		Services: map[string]config.ServiceConfig{
			"amazon": {Sync: true, From: []string{"auto-confirm@amazon.com"}, Subject: []string{"Ordered"}},
		},
		EmailAccounts: []config.EmailAccount{testAccount},
	}

	factory := func(account config.EmailAccount) (*Client, error) {
		return nil, errors.New("connection refused")
	}

	results := fetchAll(context.Background(), cfg, 7, nil, factory)
	require.Len(t, results, 1)
	require.Error(t, results[0].Err)
	assert.Contains(t, results[0].Err.Error(), "connection refused")
}

// buildAmazonEML returns a minimal but parseable Amazon order confirmation body.
func buildAmazonEML() string {
	return `From: auto-confirm@amazon.com
To: user@example.com
Subject: Your Amazon.com order of "Test Item"
Date: Fri, 27 Jun 2026 12:00:00 +0000
MIME-Version: 1.0
Content-Type: text/plain; charset=UTF-8

Order Confirmation
------------------
Order #123-4567890-1234567

Items Ordered:
1 Test Item

Order Total: $19.99
`
}
