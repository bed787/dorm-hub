package listings

import (
	"reflect"
	"testing"
	"time"
)

func TestMixedOverviewPreservesFreshnessDiversityAndSessionOrder(t *testing.T) {
	items := []Listing{}
	for i := 0; i < 30; i++ {
		author := int64(1)
		if i >= 10 {
			author = int64(i%5 + 2)
		}
		items = append(items, Listing{ID: int64(i + 1), AuthorID: author, CreatedAt: time.Unix(int64(100-i), 0)})
	}
	a := mixListings(items, "23", 8)
	b := mixListings(items, "23", 8)
	if !reflect.DeepEqual(a, b) || len(a) != 8 || a[0].ID != 1 {
		t.Fatalf("unstable order or newest missing: %+v", a)
	}
	seen := map[int64]bool{}
	authors := map[int64]bool{}
	for _, l := range a {
		if seen[l.ID] {
			t.Fatal("duplicate card")
		}
		seen[l.ID] = true
		authors[l.AuthorID] = true
	}
	if len(authors) < 5 {
		t.Fatalf("one author crowds out the rest: %+v", a)
	}
	changed := false
	for _, seed := range []string{"24", "25", "26"} {
		if !reflect.DeepEqual(a, mixListings(items, seed, 8)) {
			changed = true
		}
	}
	if !changed {
		t.Fatal("rotation never changes")
	}
	if len(mixListings(items[:2], "23", 8)) != 2 {
		t.Fatal("small category lost cards")
	}
	if len(mixListings(nil, "23", 8)) != 0 {
		t.Fatal("empty category")
	}
}
