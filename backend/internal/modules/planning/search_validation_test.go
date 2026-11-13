package planning

import "testing"

func TestSearchRejectsInvalidUTF8BeforePassingTextToPostgreSQL(t *testing.T) {
	if err := validateSearch(SearchInput{Query: string([]byte{0xff}), Limit: 20}); err == nil {
		t.Fatal("invalid UTF-8 query accepted")
	}
}
