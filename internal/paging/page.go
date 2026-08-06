// Package paging carries the list window from the HTTP boundary down to SQL.
// It exists so a list endpoint never loads a whole table into memory to slice
// it: the store receives Limit and Offset, counts the matching rows separately
// and returns only the requested page.
package paging

const (
	// DefaultSize is applied when the caller omits page_size.
	DefaultSize = 50
	// MaxSize caps a single response so one request cannot read a whole table.
	MaxSize = 200
)

// Page is a validated one-based list window. The zero value is not usable; New
// is the only way to build one, which keeps unvalidated user input out of SQL.
type Page struct {
	number int
	size   int
}

// New validates a one-based page number and size. It reports false instead of
// clamping so the API can reject the request rather than silently returning a
// window the caller did not ask for, and it applies no default: substituting
// DefaultSize for an explicit page_size=0 would hide a client bug.
func New(number, size int) (Page, bool) {
	if number < 1 || size < 1 || size > MaxSize {
		return Page{}, false
	}
	return Page{number: number, size: size}, true
}

// Number is the one-based page index.
func (page Page) Number() int { return page.number }

// Size is the maximum number of rows in the page.
func (page Page) Size() int { return page.size }

// Limit is the SQL LIMIT value.
func (page Page) Limit() int { return page.size }

// Offset is the SQL OFFSET value.
func (page Page) Offset() int { return (page.number - 1) * page.size }

// Result is the wire shape every paginated list endpoint returns.
type Result[T any] struct {
	Items    []T `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

// NewResult wraps a page that the store already narrowed. Items is normalised
// to an empty slice so the JSON body always carries an array, never null.
func NewResult[T any](items []T, page Page, total int) Result[T] {
	if items == nil {
		items = []T{}
	}
	return Result[T]{Items: items, Page: page.number, PageSize: page.size, Total: total}
}
