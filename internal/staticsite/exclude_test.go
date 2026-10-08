package staticsite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseExcludeList(t *testing.T) {
	got, err := ParseExcludeList(strings.NewReader("# снятые по жалобе\n\n100  # письмо от 01.11\n  205\n" +
		"145 apparatus  # 2026-10-07: снят аппарат по жалобе (takedown.sh apparatus)\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Works) != 2 || !got.Works[100] || !got.Works[205] {
		t.Errorf("работы = %v, хотел {100, 205}", got.Works)
	}
	if len(got.Apparatus) != 1 || !got.Apparatus[145] {
		t.Errorf("аппарат = %v, хотел {145}", got.Apparatus)
	}
	if got.Works[145] {
		t.Error("«145 apparatus» исключило том целиком")
	}
	for _, bad := range []string{"сто\n", "145 aparatus\n", "145 apparatus лишнее\n", "0\n"} {
		if _, err := ParseExcludeList(strings.NewReader(bad)); err == nil {
			t.Errorf("%q принято молча — опечатка в списке вернула бы снятое в архив", bad)
		}
	}
}

func TestExcludedVolumeLeavesNoTrace(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	g := &Generator{Src: fixture(), Out: out, BuildDate: "2026-10-06", Exclude: map[int64]bool{100: true}}
	if err := g.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{vol1, ch10, frontIdx, "editions/stalin/index.html"} {
		if exists(out, p) {
			t.Errorf("снятый том оставил %s", p)
		}
	}
	for _, p := range []string{HomeFile, assetTitles} {
		if strings.Contains(read(t, out, p), "stalin-t01") {
			t.Errorf("%s упоминает снятый том", p)
		}
	}
}
