package email

import "fmt"

// Parser extracts transactions from emails
type Parser interface {
	// Parse returns transactions found in the email body
	Parse(emailBody string) ([]Transaction, error)
	// Name identifies the parser (e.g., "amazon", "venmo")
	Name() string
}

// Registry maps service names to parsers
var parsers = make(map[string]Parser)

// Register adds a parser to the registry
func Register(p Parser) {
	if p == nil {
		panic("parser cannot be nil")
	}
	parsers[p.Name()] = p
}

// GetParser retrieves a parser by service name
func GetParser(serviceName string) Parser {
	return parsers[serviceName]
}

// ListParsers returns all available parser names
func ListParsers() []string {
	names := make([]string, 0, len(parsers))
	for name := range parsers {
		names = append(names, name)
	}
	return names
}

// ParseWithAll attempts to parse email with all registered parsers
// Returns combined results from all parsers that successfully extract transactions
func ParseWithAll(emailBody, from, subject string) map[string][]Transaction {
	return ParseWithServices(emailBody, from, subject, ListParsers())
}

// ParseWithServices attempts to parse email with a restricted set of parsers.
// If serviceNames is empty, it behaves like ParseWithAll.
func ParseWithServices(emailBody, from, subject string, serviceNames []string) map[string][]Transaction {
	results := make(map[string][]Transaction)
	if len(serviceNames) == 0 {
		serviceNames = ListParsers()
	}

	for _, name := range serviceNames {
		parser := parsers[name]
		if parser == nil {
			continue
		}

		txns, err := parser.Parse(emailBody)
		if err == nil && len(txns) > 0 {
			results[name] = txns
		}
	}

	return results
}

// ValidationError wraps parse errors with context
type ValidationError struct {
	Service string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("parse error for %s: %s", e.Service, e.Message)
}
