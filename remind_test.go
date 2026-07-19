package remind

import (
	"testing"
	"time"

	docs "google.golang.org/api/docs/v1"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestParseHeadingDate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		{"plain us", "3/10/2022", date(2022, time.March, 10), true},
		{"with weekday suffix", "3/10/2022, Thursday:", date(2022, time.March, 10), true},
		{"trailing newline", "3/10/2022\n", date(2022, time.March, 10), true},
		// The bug that caused production crashes: an invisible zero-width space.
		{"trailing zero-width space", "3/10/2022\u200b", date(2022, time.March, 10), true},
		{"leading LTR mark", "\u200e3/10/2022", date(2022, time.March, 10), true},
		{"non-breaking space", "3/10/2022\u00a0", date(2022, time.March, 10), true},
		{"surrounded by text", "Entry for 5/1/2020 below", date(2020, time.May, 1), true},
		// European day-first (day > 12 makes US parse fail, falls back to D/M/Y).
		{"european day-first", "19/7/2021, Monday:", date(2021, time.July, 19), true},
		// Ambiguous date: US ordering is tried first, so this is March 4 (locks current behavior).
		{"ambiguous prefers us", "4/3/2020", date(2020, time.April, 3), true},
		{"single digit day/month", "1/2/2019", date(2019, time.January, 2), true},
		{"no date", "Just some prose, no date", time.Time{}, false},
		{"empty", "", time.Time{}, false},
		{"only newline", "\n", time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseHeadingDate(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (input %q)", ok, c.ok, c.in)
			}
			if ok && !got.Equal(c.want) {
				t.Fatalf("date = %s, want %s (input %q)", got.Format("2006-01-02"), c.want.Format("2006-01-02"), c.in)
			}
		})
	}
}

func TestYearFromTitle(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"Journal 2021", 2021, true},
		{"Journal 2007", 2007, true},
		{"My 2019 Journal", 2019, true}, // robust to prefix changes
		{"Journal", 0, false},
		{"", 0, false},
		{"no digits here", 0, false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, ok := yearFromTitle(c.in)
			if ok != c.ok || got != c.want {
				t.Fatalf("yearFromTitle(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

// --- helpers for building docs.Document fixtures ---

func heading(text string) *docs.StructuralElement {
	return &docs.StructuralElement{Paragraph: &docs.Paragraph{
		ParagraphStyle: &docs.ParagraphStyle{NamedStyleType: "HEADING_4"},
		Elements:       []*docs.ParagraphElement{{TextRun: &docs.TextRun{Content: text}}},
	}}
}

func para(text string) *docs.StructuralElement {
	return &docs.StructuralElement{Paragraph: &docs.Paragraph{
		ParagraphStyle: &docs.ParagraphStyle{NamedStyleType: "NORMAL_TEXT"},
		Elements:       []*docs.ParagraphElement{{TextRun: &docs.TextRun{Content: text}}},
	}}
}

func pageBreakPara() *docs.StructuralElement {
	return &docs.StructuralElement{Paragraph: &docs.Paragraph{
		ParagraphStyle: &docs.ParagraphStyle{NamedStyleType: "NORMAL_TEXT"},
		Elements:       []*docs.ParagraphElement{{PageBreak: &docs.PageBreak{}}},
	}}
}

func makeDoc(items ...*docs.StructuralElement) *docs.Document {
	return &docs.Document{Body: &docs.Body{Content: items}}
}

func TestExtractEntryForDate(t *testing.T) {
	doc := makeDoc(
		heading("7/18/2021, Sunday:\n"),
		para("Yesterday's entry.\n"),
		heading("7/19/2021, Monday:\n"),
		para("Woke up in pain.\n"),
		para("Went to work.\n"),
		heading("7/20/2021, Tuesday:\n"),
		para("Tomorrow's entry.\n"),
	)

	t.Run("returns matching entry incl heading, stops at next heading", func(t *testing.T) {
		msg := extractEntryForDate(doc, date(2021, time.July, 19))
		want := "7/19/2021, Monday:\nWoke up in pain.\nWent to work.\n"
		if msg != want {
			t.Fatalf("got %q, want %q", msg, want)
		}
	})

	t.Run("no entry for date returns empty", func(t *testing.T) {
		if msg := extractEntryForDate(doc, date(2021, time.December, 25)); msg != "" {
			t.Fatalf("got %q, want empty", msg)
		}
	})

	t.Run("page break terminates the entry", func(t *testing.T) {
		d := makeDoc(
			heading("1/1/2020, Wednesday:\n"),
			para("First line.\n"),
			pageBreakPara(),
			para("After the break, should not appear.\n"),
		)
		msg := extractEntryForDate(d, date(2020, time.January, 1))
		want := "1/1/2020, Wednesday:\nFirst line.\n"
		if msg != want {
			t.Fatalf("got %q, want %q", msg, want)
		}
	})

	t.Run("handles nil document safely", func(t *testing.T) {
		if msg := extractEntryForDate(nil, date(2021, time.July, 19)); msg != "" {
			t.Fatalf("got %q, want empty", msg)
		}
	})

	t.Run("skips heading with no elements without panicking", func(t *testing.T) {
		d := makeDoc(
			&docs.StructuralElement{Paragraph: &docs.Paragraph{
				ParagraphStyle: &docs.ParagraphStyle{NamedStyleType: "HEADING_4"},
			}},
			heading("6/15/2018, Friday:\n"),
			para("The entry.\n"),
		)
		msg := extractEntryForDate(d, date(2018, time.June, 15))
		want := "6/15/2018, Friday:\nThe entry.\n"
		if msg != want {
			t.Fatalf("got %q, want %q", msg, want)
		}
	})
}
