package opds

import "testing"

func TestParsePath(t *testing.T) {
	cases := []struct {
		path string
		want Route
		ok   bool
	}{
		{"/opds", Route{Kind: KindRoot}, true},
		{"/opds/", Route{Kind: KindRoot}, true},
		{"/opds/editions", Route{Kind: KindEditions}, true},
		{"/opds/editions/7", Route{Kind: KindEdition, EditionID: 7}, true},
		{"/opds/loose", Route{Kind: KindLoose}, true},
		{"/opds/new", Route{Kind: KindNew}, true},
		{"/opds/search", Route{Kind: KindSearch}, true},
		{"/opds/search.xml", Route{Kind: KindOpenSearch}, true},
		{"/opds/works/252", Route{Kind: KindWork, WorkID: 252}, true},
		{"/opds/works/252/chapters/10", Route{Kind: KindChapter, WorkID: 252, ChapterID: 10}, true},
		{"/opds/works/0", Route{}, false},
		{"/opds/works/-1", Route{}, false},
		{"/opds/works/+5", Route{}, false},
		{"/opds/works/49-lenin-t06", Route{}, false},
		{"/opds/works/252/chapters", Route{}, false},
		{"/opds/works/252/pages/1", Route{}, false},
		{"/opds/editions/x", Route{}, false},
		{"/opdsx", Route{}, false},
		{"/opdseditions", Route{}, false},
		{"/opds//editions", Route{}, false},
		{"/opds/unknown", Route{}, false},
		{"/seo/opds", Route{}, false},
	}
	for _, c := range cases {
		got, ok := ParsePath(c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("ParsePath(%q) = %+v, %v; ждали %+v, %v", c.path, got, ok, c.want, c.ok)
		}
	}
}
