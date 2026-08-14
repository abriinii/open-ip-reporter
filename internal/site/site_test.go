package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultIsThisSite(t *testing.T) {
	s := Default()
	if err := s.Validate(); err != nil {
		t.Fatalf("the shipped default does not validate: %v", err)
	}
	for _, name := range []string{"A1", "B4", "O3"} {
		if !s.Has(name) {
			t.Errorf("default site is missing %s", name)
		}
	}
	// A3 and A4 are out of commission, and 34.x is the testbench, not O4.
	for _, name := range []string{"A3", "A4", "O4"} {
		if s.Has(name) {
			t.Errorf("default site offers %s, which is not walkable", name)
		}
	}
	if g, _ := s.Geometry("B1"); g.Positions() != 50 {
		t.Errorf("B1 has %d positions, want 50", g.Positions())
	}
	if g, _ := s.Geometry("O1"); g.Positions() != 48 {
		t.Errorf("O1 has %d positions, want 48", g.Positions())
	}
}

// The whole point of this package: a different site's layout works without
// touching the code, including rack shapes larger than anything here.
func TestADifferentSiteWorks(t *testing.T) {
	other := Site{Cans: []Can{
		{Name: "Shed", Rows: 12, Columns: 8}, // 96, larger than any rack here
		{Name: "Barn2", Rows: 4, Columns: 3},
	}}
	if err := other.Validate(); err != nil {
		t.Fatalf("a valid foreign layout was rejected: %v", err)
	}
	g, ok := other.Geometry("Shed")
	if !ok || g.Rows != 12 || g.Columns != 8 {
		t.Errorf("Geometry(Shed) = %v, %v", g, ok)
	}
	if other.Has("B1") {
		t.Error("a foreign site reports having this site's cans")
	}
}

func TestGeometryLookupIsCaseInsensitive(t *testing.T) {
	s := Default()
	if _, ok := s.Geometry("b1"); !ok {
		t.Error("lowercase can name not found")
	}
}

func TestValidateRejectsWhatAPersonWouldTypoWrong(t *testing.T) {
	cases := map[string]Site{
		"empty list":     {},
		"no name":        {Cans: []Can{{Name: "  ", Rows: 10, Columns: 5}}},
		"zero rows":      {Cans: []Can{{Name: "A1", Rows: 0, Columns: 5}}},
		"negative cols":  {Cans: []Can{{Name: "A1", Rows: 10, Columns: -2}}},
		"duplicate name": {Cans: []Can{{Name: "A1", Rows: 10, Columns: 5}, {Name: "a1", Rows: 8, Columns: 6}}},
		"absurd size":    {Cans: []Can{{Name: "A1", Rows: 9999, Columns: 9999}}},
	}
	for label, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted, want rejection", label)
		}
	}
}

// The error message is the entire report to someone editing this by hand, so
// it has to name the offending can.
func TestValidateSaysWhichCanIsWrong(t *testing.T) {
	s := Site{Cans: []Can{
		{Name: "A1", Rows: 10, Columns: 5},
		{Name: "WeirdOne", Rows: 0, Columns: 5},
	}}
	err := s.Validate()
	if err == nil {
		t.Fatal("accepted an invalid can")
	}
	if !strings.Contains(err.Error(), "WeirdOne") {
		t.Errorf("error %q does not name the bad can", err)
	}
}

func TestNormaliseTidiesHandEditedInput(t *testing.T) {
	s := Site{Cans: []Can{
		{Name: "  B2  ", Rows: 10, Columns: 5},
		{Name: "", Rows: 0, Columns: 0}, // a blank row left in the editor
		{Name: "A10", Rows: 10, Columns: 5},
		{Name: "A2", Rows: 10, Columns: 5},
	}}
	got := s.Normalise()

	if len(got.Cans) != 3 {
		t.Fatalf("got %d cans, want 3 with the blank dropped", len(got.Cans))
	}
	names := got.Names()
	want := []string{"A2", "A10", "B2"} // A10 after A2, not before it
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("order = %v, want %v", names, want)
			break
		}
	}
}

// A fresh install has no file. That is not an error: it seeds the defaults and
// writes them out so there is something to edit.
func TestLoadSeedsDefaultsOnFirstRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cans.json")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Cans) != len(Default().Cans) {
		t.Errorf("seeded %d cans, want the defaults", len(s.Cans))
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("first run did not write a file to edit: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cans.json")
	want := Site{Cans: []Can{
		{Name: "North", Rows: 6, Columns: 4},
		{Name: "South", Rows: 12, Columns: 2},
	}}
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cans) != 2 || got.Cans[0] != want.Cans[0] || got.Cans[1] != want.Cans[1] {
		t.Errorf("round-tripped as %+v", got.Cans)
	}
}

// A corrupt file must be reported, not silently replaced with defaults — that
// would quietly discard a layout someone spent time entering.
func TestLoadRefusesGarbageRatherThanResetting(t *testing.T) {
	dir := t.TempDir()

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o644)
	if _, err := Load(bad); err == nil {
		t.Error("invalid JSON loaded without error")
	}

	invalid := filepath.Join(dir, "invalid.json")
	os.WriteFile(invalid, []byte(`{"cans":[{"name":"A1","rows":0,"columns":5}]}`), 0o644)
	if _, err := Load(invalid); err == nil {
		t.Error("a structurally valid but nonsensical layout loaded without error")
	}
}

func TestSaveRefusesToWriteAnInvalidLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cans.json")
	if err := (Site{}).Save(path); err == nil {
		t.Fatal("saved an empty layout")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("an invalid layout still created a file")
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "*")); len(entries) != 0 {
		t.Errorf("left files behind: %v", entries)
	}
}
