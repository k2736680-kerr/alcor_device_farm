package api

import (
	"net/http"
	"strconv"

	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
)

// pagination parses the list window from the query string. It deliberately
// returns no fallback page on bad input so the handler answers 400 instead of
// quietly serving a different window than the caller requested.
//
// There is no in-memory slicing helper next to this function on purpose: every
// list endpoint must push the window down to SQL.
func pagination(request *http.Request) (paging.Page, bool) {
	number, size := 1, paging.DefaultSize
	if raw := request.URL.Query().Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return paging.Page{}, false
		}
		number = value
	}
	if raw := request.URL.Query().Get("page_size"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return paging.Page{}, false
		}
		size = value
	}
	return paging.New(number, size)
}
