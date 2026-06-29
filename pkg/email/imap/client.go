package imap

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"ynam/pkg/config"
	"ynam/pkg/email"

	imapv2 "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// FetchResult holds the parsed transactions found in a single email account
// along with any error encountered while processing that account.
type FetchResult struct {
	Account      string              // Email address the result came from
	Transactions []email.Transaction // All transactions parsed from this account
	MatchedCount int                 // Number of IMAP messages that matched filters
	Err          error               // Non-nil if the account failed entirely
}

// Client wraps an IMAP connection for a single email account.
type Client struct {
	account config.EmailAccount
	conn    Conn
}

// dialTimeout is the maximum time to wait when establishing a connection.
const dialTimeout = 30 * time.Second

// NewClient connects and authenticates to the IMAP server for an account.
// The caller must call Close when finished.
func NewClient(account config.EmailAccount) (*Client, error) {
	addr := account.IMAPServer
	// Default to the implicit TLS IMAP port if none was supplied.
	if !strings.Contains(addr, ":") {
		addr += ":993"
	}

	raw, err := imapclient.DialTLS(addr, &imapclient.Options{})
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	if err := raw.Login(account.Email, account.Password).Wait(); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("login failed: %w", err)
	}

	return &Client{account: account, conn: &realConn{c: raw}}, nil
}

// NewClientWithConn creates a Client backed by an already-authenticated Conn.
// Intended for tests that inject a mock Conn.
func NewClientWithConn(account config.EmailAccount, conn Conn) *Client {
	return &Client{account: account, conn: conn}
}

// Close logs out and tears down the IMAP connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	// Best-effort logout, then close.
	_ = c.conn.Logout()
	return c.conn.Close()
}

// FetchSince searches every mailbox for messages received on or after the given
// date, parses them, and returns the transactions plus the raw match count.
// Messages appearing in more than one mailbox (e.g. Gmail's "All Mail") are
// de-duplicated by Message-ID.
func (c *Client) FetchSince(since, until time.Time, services []string, filters map[string]config.ServiceConfig) ([]email.Transaction, int, error) {
	msgs, count, err := c.FetchWithBodies(since, until, services, filters)
	if err != nil {
		return nil, 0, err
	}
	var txns []email.Transaction
	for _, m := range msgs {
		txns = append(txns, m.Transactions...)
	}
	return txns, count, nil
}

// SearchSince returns matching message UIDs across all mailboxes. UIDs are only
// unique within a mailbox, so the combined slice is suitable for counting, not
// for addressing specific messages.
func (c *Client) SearchSince(since time.Time, services []string, filters map[string]config.ServiceConfig) ([]imapv2.UID, error) {
	if len(services) == 0 {
		services = email.ListParsers()
	}

	boxes := c.selectableMailboxes()
	var all []imapv2.UID
	for _, box := range boxes {
		if err := c.conn.Select(box); err != nil {
			continue // skip mailboxes we cannot open
		}
		uids, err := c.searchUIDsRetry(since, time.Time{}, services, filters)
		if err != nil {
			continue // skip a busy/failing mailbox
		}
		all = append(all, uids...)
	}
	return all, nil
}

// selectableMailboxes returns the mailboxes configured for this account. The
// config layer guarantees at least one is present, but we fall back to INBOX
// defensively for clients constructed without a configured account (e.g. tests).
func (c *Client) selectableMailboxes() []string {
	if len(c.account.Mailboxes) == 0 {
		return []string{"INBOX"}
	}
	return c.account.Mailboxes
}

// searchUIDs searches for messages by date, sender AND subject criteria.
// `until` is an inclusive upper bound; since IMAP's BEFORE is exclusive, we add
// one day. A zero `until` means no upper bound.
func (c *Client) searchUIDs(since, until time.Time, services []string, filters map[string]config.ServiceConfig) ([]imapv2.UID, error) {
	criteria := &imapv2.SearchCriteria{Since: since}
	if !until.IsZero() {
		criteria.Before = until.AddDate(0, 0, 1)
	}
	clauses := make([]imapv2.SearchCriteria, 0, len(services)*2)

	for _, service := range services {
		service = strings.ToLower(strings.TrimSpace(service))
		filter, ok := filters[service]
		if !ok {
			continue
		}

		for _, from := range uniqueStrings(filter.From) {
			clauses = append(clauses, imapv2.SearchCriteria{
				Header: []imapv2.SearchCriteriaHeaderField{{Key: "FROM", Value: from}},
			})
		}
		for _, subject := range uniqueStrings(filter.Subject) {
			clauses = append(clauses, imapv2.SearchCriteria{
				Header: []imapv2.SearchCriteriaHeaderField{{Key: "SUBJECT", Value: subject}},
			})
		}
	}

	if len(clauses) > 0 {
		criteria.Or = buildOrSearchCriteria(clauses)
	}

	uids, err := c.conn.UIDSearch(criteria)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return uids, nil
}

// buildOrSearchCriteria builds a right-nested OR tree: OR a (OR b (OR c d)).
// go-imap/v2 treats multiple pairs in criteria.Or as AND, so we must produce
// exactly one pair at the top level and nest the remainder on the right.
func buildOrSearchCriteria(clauses []imapv2.SearchCriteria) [][2]imapv2.SearchCriteria {
	if len(clauses) == 0 {
		return nil
	}
	if len(clauses) == 1 {
		return [][2]imapv2.SearchCriteria{{clauses[0], clauses[0]}}
	}
	// Build right-to-left so we end with OR clauses[0] (OR clauses[1] ...).
	right := clauses[len(clauses)-1]
	for i := len(clauses) - 2; i >= 1; i-- {
		right = imapv2.SearchCriteria{Or: [][2]imapv2.SearchCriteria{{clauses[i], right}}}
	}
	return [][2]imapv2.SearchCriteria{{clauses[0], right}}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// ParsedMessage holds a parsed email alongside its raw body bytes.
type ParsedMessage struct {
	Transactions []email.Transaction
	RawBody      string
	From         string
	Subject      string
	MessageID    string // RFC822 Message-ID, used to de-duplicate across mailboxes
	Mailbox      string // mailbox the message was found in (set by diagnostic search)
}

// FetchWithBodies searches every mailbox and returns ParsedMessage entries so
// callers can access the raw email body alongside parsed transactions. Messages
// that appear in multiple mailboxes (e.g. Gmail's "All Mail" duplicates the
// INBOX) are de-duplicated by Message-ID. The returned count is the number of
// unique messages fetched.
func (c *Client) FetchWithBodies(since, until time.Time, services []string, filters map[string]config.ServiceConfig) ([]ParsedMessage, int, error) {
	if len(services) == 0 {
		services = email.ListParsers()
	}

	boxes := c.selectableMailboxes()

	var out []ParsedMessage
	var lastErr error
	succeeded := 0
	seen := make(map[string]bool)
	for _, box := range boxes {
		if err := c.conn.Select(box); err != nil {
			lastErr = err
			continue // skip mailboxes we cannot open
		}
		uids, err := c.searchUIDsRetry(since, until, services, filters)
		if err != nil {
			lastErr = err
			continue // a busy/failing mailbox shouldn't abort the whole account
		}
		if len(uids) == 0 {
			succeeded++ // mailbox processed cleanly, just no matches
			continue
		}
		msgs, err := c.fetchAndParseRaw(uids, services, filters)
		if err != nil {
			lastErr = err
			continue
		}
		succeeded++
		for _, m := range msgs {
			// De-duplicate by Message-ID across mailboxes. Messages with no
			// Message-ID are always kept (cannot prove they're duplicates).
			if m.MessageID != "" {
				if seen[m.MessageID] {
					continue
				}
				seen[m.MessageID] = true
			}
			out = append(out, m)
		}
	}
	// Only fail the account if no mailbox could be searched at all.
	if succeeded == 0 && lastErr != nil {
		return nil, 0, lastErr
	}
	return out, len(out), nil
}

// FindText searches EVERY mailbox on the account (not just configured ones) for
// messages whose text contains query, using a server-side IMAP TEXT search. It
// parses each match with all registered parsers. Intended as a diagnostic to
// check whether a specific order/transaction appears anywhere in the mailbox.
func (c *Client) FindText(query string) ([]ParsedMessage, error) {
	boxes, err := c.conn.ListMailboxes()
	if err != nil || len(boxes) == 0 {
		boxes = c.selectableMailboxes()
	}

	var out []ParsedMessage
	seen := make(map[string]bool)
	for _, box := range boxes {
		if err := c.conn.Select(box); err != nil {
			continue
		}
		uids, err := c.conn.UIDSearch(&imapv2.SearchCriteria{Text: []string{query}})
		if err != nil || len(uids) == 0 {
			continue
		}
		msgs, err := c.fetchAndParseRaw(uids, email.ListParsers(), nil)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if m.MessageID != "" {
				if seen[m.MessageID] {
					continue
				}
				seen[m.MessageID] = true
			}
			m.Mailbox = box
			out = append(out, m)
		}
	}
	return out, nil
}

// FindByText searches every configured account (all of each account's mailboxes)
// for messages containing query, returning per-account results.
func FindByText(ctx context.Context, cfg *config.Config, query string) []RawFetchResult {
	accounts := cfg.GetEmailAccounts()
	results := make([]RawFetchResult, len(accounts))
	var wg sync.WaitGroup

	for i, account := range accounts {
		i, account := i, account
		wg.Add(1)
		go func() {
			defer wg.Done()
			tCtx, cancel := context.WithTimeout(ctx, perAccountTimeout)
			defer cancel()

			res := RawFetchResult{Account: account.Email}
			client, err := NewClient(account)
			if err != nil {
				res.Err = err
				results[i] = res
				return
			}
			go func() {
				<-tCtx.Done()
				_ = client.conn.Close()
			}()
			defer client.Close()

			msgs, err := client.FindText(query)
			if err != nil {
				res.Err = err
			} else {
				res.Messages = msgs
				res.MatchedCount = len(msgs)
			}
			results[i] = res
		}()
	}

	wg.Wait()
	return results
}

// searchUIDsRetry runs searchUIDs, retrying once after a short pause when the
// server reports a transient condition (e.g. iCloud's "Server Busy").
func (c *Client) searchUIDsRetry(since, until time.Time, services []string, filters map[string]config.ServiceConfig) ([]imapv2.UID, error) {
	uids, err := c.searchUIDs(since, until, services, filters)
	if err != nil && isTransientIMAPError(err) {
		time.Sleep(2 * time.Second)
		return c.searchUIDs(since, until, services, filters)
	}
	return uids, err
}

// isTransientIMAPError reports whether an IMAP error is likely temporary and
// worth retrying (server busy / temporarily unavailable).
func isTransientIMAPError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "busy") || strings.Contains(msg, "unavailable") ||
		strings.Contains(msg, "try again")
}

// fetchAndParseRaw is the shared implementation used by fetchAndParse and
// FetchWithBodies. It returns one ParsedMessage per email that has a body.
func (c *Client) fetchAndParseRaw(uids []imapv2.UID, services []string, filters map[string]config.ServiceConfig) ([]ParsedMessage, error) {
	var uidSet imapv2.UIDSet
	for _, uid := range uids {
		uidSet.AddNum(uid)
	}

	fetchOptions := &imapv2.FetchOptions{
		Envelope: true,
		BodySection: []*imapv2.FetchItemBodySection{
			{}, // Empty section fetches the entire message body.
		},
	}

	messages, err := c.conn.FetchMessages(uidSet, fetchOptions)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	var out []ParsedMessage
	for _, msg := range messages {
		from := envelopeFrom(msg.Envelope)
		subject := ""
		if msg.Envelope != nil {
			subject = msg.Envelope.Subject
		}

		body := bodyText(msg)
		if body == "" {
			continue
		}

		// Only run parsers whose from/subject filters match this specific
		// message. Without this gate every parser runs on every email, causing
		// cross-contamination (e.g. Apple parser firing on Amazon emails).
		matched := servicesForMessage(from, subject, filters)
		if len(matched) == 0 {
			matched = services // fallback: no filters provided, try all
		}

		results := email.ParseWithServices(body, from, subject, matched)
		var txns []email.Transaction
		for service, parsed := range results {
			for i := range parsed {
				if parsed[i].Service == "" {
					parsed[i].Service = service
				}
			}
			txns = append(txns, parsed...)
		}
		messageID := ""
		if msg.Envelope != nil {
			messageID = msg.Envelope.MessageID
		}
		out = append(out, ParsedMessage{
			Transactions: txns,
			RawBody:      body,
			From:         from,
			Subject:      subject,
			MessageID:    messageID,
		})
	}

	return out, nil
}

// servicesForMessage returns the subset of filter keys whose From or Subject
// patterns match the given message envelope fields. An email is attributed to
// a service when at least one of its configured From addresses is a suffix of
// the message's From address, or one of its Subject keywords appears
// (case-insensitively) in the message subject.
func servicesForMessage(from, subject string, filters map[string]config.ServiceConfig) []string {
	if len(filters) == 0 {
		return nil
	}
	fromL := strings.ToLower(from)
	subjectL := strings.ToLower(subject)

	var matched []string
	for service, filter := range filters {
		for _, addr := range filter.From {
			if strings.Contains(fromL, strings.ToLower(strings.TrimSpace(addr))) {
				matched = append(matched, service)
				goto next
			}
		}
		for _, kw := range filter.Subject {
			if strings.Contains(subjectL, strings.ToLower(strings.TrimSpace(kw))) {
				matched = append(matched, service)
				goto next
			}
		}
	next:
	}
	return matched
}

// envelopeFrom extracts a usable "from" address from a message envelope.
func envelopeFrom(env *imapv2.Envelope) string {
	if env == nil || len(env.From) == 0 {
		return ""
	}
	addr := env.From[0]
	if addr.Mailbox != "" && addr.Host != "" {
		return fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
	}
	return addr.Name
}

// bodyText returns the first non-empty body section as a string.
func bodyText(msg *imapclient.FetchMessageBuffer) string {
	for _, section := range msg.BodySection {
		if len(section.Bytes) > 0 {
			return string(section.Bytes)
		}
	}
	return ""
}

// clientFactory is a function that creates an authenticated IMAP client.
// Parameterised so tests can inject a pre-built client backed by a mock Conn.
type clientFactory func(config.EmailAccount) (*Client, error)

// perAccountTimeout is the maximum time allowed for a single account's
// IMAP connection, search, and fetch. Searching every mailbox (and Gmail's
// "All Mail", which duplicates the inbox) is substantially slower than a single
// INBOX scan, so this is generous; a hung server still won't block others.
const perAccountTimeout = 300 * time.Second

// FetchAll connects to every configured email account concurrently, fetches
// messages since the lookback period, and returns per-account results in
// config order. A failure in one account does not prevent others from
// completing. onDone is called from each account's goroutine the moment it
// finishes — use it to update a spinner or progress display. May be nil.
func FetchAll(ctx context.Context, cfg *config.Config, daysToLookBack int, onDone func(idx int, result FetchResult)) []FetchResult {
	return fetchAll(ctx, cfg, daysToLookBack, onDone, NewClient)
}

// FetchAllRange is like FetchAll but fetches messages within [since, until]
// (inclusive). A zero `until` means no upper bound.
func FetchAllRange(ctx context.Context, cfg *config.Config, since, until time.Time, onDone func(idx int, result FetchResult)) []FetchResult {
	return fetchAllRange(ctx, cfg, since, until, onDone, NewClient)
}

// fetchAll is the internal days-based wrapper used by tests and FetchAll.
func fetchAll(ctx context.Context, cfg *config.Config, daysToLookBack int, onDone func(idx int, result FetchResult), dial clientFactory) []FetchResult {
	return fetchAllRange(ctx, cfg, cfg.SinceDateAsTime(daysToLookBack), time.Time{}, onDone, dial)
}

// fetchAllRange is the internal implementation that accepts a clientFactory so
// tests can inject a pre-built client without dialing a real IMAP server.
func fetchAllRange(ctx context.Context, cfg *config.Config, since, until time.Time, onDone func(idx int, result FetchResult), dial clientFactory) []FetchResult {
	accounts := cfg.GetEmailAccounts()

	results := make([]FetchResult, len(accounts))
	var wg sync.WaitGroup

	for i, account := range accounts {
		i, account := i, account
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each account gets its own deadline, child of the root ctx so
			// Ctrl+C still cancels everything immediately.
			tCtx, cancel := context.WithTimeout(ctx, perAccountTimeout)
			defer cancel()

			result := processAccount(tCtx, cfg, account, since, until, dial)
			results[i] = result
			if onDone != nil {
				onDone(i, result)
			}
		}()
	}

	wg.Wait()
	return results
}

// processAccount runs the full IMAP flow for one account and returns its result.
func processAccount(ctx context.Context, cfg *config.Config, account config.EmailAccount, since, until time.Time, dial clientFactory) FetchResult {
	result := FetchResult{Account: account.Email}

	services := cfg.ServicesForAccount(account)
	if len(services) == 0 {
		result.Err = fmt.Errorf("no enabled services configured")
		return result
	}

	if ctx.Err() != nil {
		result.Err = ctx.Err()
		return result
	}

	client, err := dial(account)
	if err != nil {
		result.Err = err
		return result
	}

	// Close the IMAP connection when the context is cancelled so that any
	// blocking Wait() call inside FetchSince unblocks immediately.
	go func() {
		<-ctx.Done()
		_ = client.conn.Close()
	}()
	defer client.Close()

	filters := cfg.FiltersForServices(services)
	txns, matched, err := client.FetchSince(since, until, services, filters)
	if err != nil {
		if ctx.Err() != nil {
			result.Err = fmt.Errorf("cancelled")
		} else {
			result.Err = err
		}
		return result
	}

	result.Transactions = txns
	result.MatchedCount = matched
	return result
}

// RawFetchResult is like FetchResult but also carries the raw parsed messages
// (including body text) so callers can inspect or save the raw email content.
type RawFetchResult struct {
	Account      string
	Messages     []ParsedMessage
	MatchedCount int
	Err          error
}

// FetchAllWithBodies is like FetchAll but returns RawFetchResult so callers
// can access the raw email body alongside parsed transactions.
func FetchAllWithBodies(ctx context.Context, cfg *config.Config, daysToLookBack int, onDone func(idx int, result RawFetchResult)) []RawFetchResult {
	return FetchAllWithBodiesRange(ctx, cfg, cfg.SinceDateAsTime(daysToLookBack), time.Time{}, onDone)
}

// FetchAllWithBodiesRange is like FetchAllWithBodies but fetches messages within
// [since, until] (inclusive). A zero `until` means no upper bound.
func FetchAllWithBodiesRange(ctx context.Context, cfg *config.Config, since, until time.Time, onDone func(idx int, result RawFetchResult)) []RawFetchResult {
	accounts := cfg.GetEmailAccounts()

	results := make([]RawFetchResult, len(accounts))
	var wg sync.WaitGroup

	for i, account := range accounts {
		i, account := i, account
		wg.Add(1)
		go func() {
			defer wg.Done()
			tCtx, cancel := context.WithTimeout(ctx, perAccountTimeout)
			defer cancel()

			result := processAccountWithBodies(tCtx, cfg, account, since, until)
			results[i] = result
			if onDone != nil {
				onDone(i, result)
			}
		}()
	}

	wg.Wait()
	return results
}

func processAccountWithBodies(ctx context.Context, cfg *config.Config, account config.EmailAccount, since, until time.Time) RawFetchResult {
	result := RawFetchResult{Account: account.Email}

	services := cfg.ServicesForAccount(account)
	if len(services) == 0 {
		result.Err = fmt.Errorf("no enabled services configured")
		return result
	}

	if ctx.Err() != nil {
		result.Err = ctx.Err()
		return result
	}

	client, err := NewClient(account)
	if err != nil {
		result.Err = err
		return result
	}

	go func() {
		<-ctx.Done()
		_ = client.conn.Close()
	}()
	defer client.Close()

	filters := cfg.FiltersForServices(services)
	msgs, matched, err := client.FetchWithBodies(since, until, services, filters)
	if err != nil {
		if ctx.Err() != nil {
			result.Err = fmt.Errorf("cancelled")
		} else {
			result.Err = err
		}
		return result
	}

	result.Messages = msgs
	result.MatchedCount = matched
	return result
}

// ensure io is referenced (used indirectly by go-imap literal readers in some
// builds); kept to avoid accidental import churn.
var _ = io.Discard
