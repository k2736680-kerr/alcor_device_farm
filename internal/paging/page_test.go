package paging_test

import (
	"encoding/json"
	"testing"

	"github.com/Ad-Quanta/alcor-device-farm/internal/paging"
)

func TestNewRejectsWindowsOutsideTheAllowedRange(t *testing.T) {
	cases := map[string]struct{ number, size int }{
		"zero page":     {number: 0, size: 10},
		"negative page": {number: -1, size: 10},
		"zero size":     {number: 1, size: 0},
		"negative size": {number: 1, size: -1},
		"oversized":     {number: 1, size: paging.MaxSize + 1},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := paging.New(test.number, test.size); ok {
				t.Fatalf("New(%d, %d) must be rejected", test.number, test.size)
			}
		})
	}
}

func TestNewAcceptsTheMaximumSize(t *testing.T) {
	if _, ok := paging.New(1, paging.MaxSize); !ok {
		t.Fatalf("size %d must be accepted", paging.MaxSize)
	}
}

func TestLimitAndOffsetTranslateToSQLWindows(t *testing.T) {
	cases := []struct {
		number, size  int
		limit, offset int
	}{
		{number: 1, size: 50, limit: 50, offset: 0},
		{number: 2, size: 50, limit: 50, offset: 50},
		{number: 7, size: 25, limit: 25, offset: 150},
	}
	for _, test := range cases {
		page, ok := paging.New(test.number, test.size)
		if !ok {
			t.Fatalf("New(%d, %d) rejected", test.number, test.size)
		}
		if page.Limit() != test.limit || page.Offset() != test.offset {
			t.Fatalf("page %d size %d -> limit=%d offset=%d, want limit=%d offset=%d",
				test.number, test.size, page.Limit(), page.Offset(), test.limit, test.offset)
		}
	}
}

func TestResultReportsTheDatabaseTotalNotThePageLength(t *testing.T) {
	page, _ := paging.New(2, 2)
	result := paging.NewResult([]string{"c", "d"}, page, 9)
	if result.Total != 9 {
		t.Fatalf("total = %d, want the number of matching rows 9", result.Total)
	}
	if result.Page != 2 || result.PageSize != 2 {
		t.Fatalf("page = %d/%d, want 2/2", result.Page, result.PageSize)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(result.Items))
	}
}

func TestResultSerialisesAnEmptyPageAsAnArray(t *testing.T) {
	page, _ := paging.New(9, 10)
	body, err := json.Marshal(paging.NewResult[string](nil, page, 3))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"items":[],"page":9,"page_size":10,"total":3}`
	if string(body) != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
}

func TestZeroPageIsNotUsableAsASQLWindow(t *testing.T) {
	var page paging.Page
	if page.Limit() != 0 {
		t.Fatalf("zero page limit = %d, want 0 so an unvalidated window returns no rows", page.Limit())
	}
}
