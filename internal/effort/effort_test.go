package effort

import (
	"testing"
	"time"
)

// The model is a claim about the real world, so the tests are written as
// claims about the real world too: a one-line fix is minutes, a regenerated
// lockfile is minutes however enormous it is, and a rewritten module is days.
// A change to a constant that breaks one of these has changed what the figure
// MEANS, which is exactly when someone should have to look at it.

func TestClassifyNamesTheFileKind(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"internal/cockpit/velocity.go", "source"},
		{"src/app/Button.tsx", "source"},
		{"flake.nix", "source"},

		{"internal/effort/effort_test.go", "test"},
		{"src/components/Button.test.tsx", "test"},
		{"spec/models/user_spec.rb", "test"},
		{"tests/integration/login.py", "test"},
		{"api/tests_test.go", "test"},
		{"app/test_billing.py", "test"},

		{"go.sum", "generated"},
		{"web/package-lock.json", "generated"},
		{"flake.lock", "generated"},
		{"api/schema.pb.go", "generated"},
		{"vendor/github.com/x/y/z.go", "generated"},
		{"web/node_modules/left-pad/index.js", "generated"},
		{"web/dist/bundle.js", "generated"},
		{"assets/app.min.css", "generated"},
		{"internal/mocks/zz_generated_store.go", "generated"},

		{"testdata/big.txt", "data"},
		{"internal/gh/testdata/pr.json", "data"},
		{"config/seed.csv", "data"},
		{"docs/triage.svg", "data"},

		{"README.md", "docs"},
		{"docs/adr/0001-worktrees.md", "docs"},
		{"LICENSE", "docs"},

		{".github/workflows/ci.yml", "config"},
		{"Dockerfile", "config"},
		{"Makefile", "config"},
		{".gitignore", "config"},
		{".prettierrc", "config"},
	}
	for _, tc := range cases {
		if got := classify(tc.path).name; got != tc.want {
			t.Errorf("classify(%q) = %s, want %s", tc.path, got, tc.want)
		}
	}
}

// A modified line arrives as one added and one deleted. Charging for both
// would price every in-place edit at nearly twice what it was.
func TestChurnIsNotChargedTwice(t *testing.T) {
	rewrite := Of([]File{{"a.go", 200, 200}})
	fresh := Of([]File{{"a.go", 200, 0}})
	if rewrite.Dur != fresh.Dur {
		t.Errorf("rewriting 200 lines (%s) should cost the same as writing 200 (%s)",
			Human(rewrite.Dur), Human(fresh.Dur))
	}
	if rewrite.Lines != 200 {
		t.Errorf("chargeable lines = %d, want 200", rewrite.Lines)
	}
}

// Deleting is cheaper than writing, and is not free: proving a module is dead
// is work, it is just not the same work as writing it.
func TestPureDeletionCostsLessThanWriting(t *testing.T) {
	del := Of([]File{{"a.go", 0, 1000}})
	write := Of([]File{{"a.go", 1000, 0}})
	if del.Dur >= write.Dur {
		t.Errorf("deleting 1000 lines (%s) should cost less than writing them (%s)",
			Human(del.Dur), Human(write.Dur))
	}
	if del.Dur <= 0 {
		t.Error("deleting 1000 lines should cost something")
	}
}

// The failure this exists to prevent: a lockfile refresh reading as a week of
// work. Forty thousand emitted lines cost a person one command.
func TestGeneratedLinesCostNobodyAnything(t *testing.T) {
	lock := Of([]File{{"package-lock.json", 40000, 38000}})
	if lock.Dur > 5*time.Minute {
		t.Errorf("a regenerated lockfile estimated at %s, want the cost of running one command",
			Human(lock.Dur))
	}
	// The lines are still reported: the figure is a model, and a reader who
	// wants to know why it is small can see what it was computed from.
	if lock.Files != 1 || lock.Lines == 0 {
		t.Errorf("the diff should still be counted: %+v", lock)
	}
}

// Touching a file costs something before a single line is written, so the same
// lines spread over twenty files cost more than in one.
func TestSpreadingLinesOverFilesCostsMore(t *testing.T) {
	one := Of([]File{{"a.go", 200, 0}})
	var many []File
	for i := 0; i < 20; i++ {
		many = append(many, File{"pkg/f" + itoa(i) + ".go", 10, 0})
	}
	if Of(many).Dur <= one.Dur {
		t.Error("200 lines across 20 files should cost more than 200 lines in one")
	}
}

// Sanity anchors, in the units a human argues in. These are the numbers a
// reader is entitled to disagree with — they must not drift silently.
func TestEstimatesLandWhereAHumanWouldPutThem(t *testing.T) {
	cases := []struct {
		what            string
		diff            []File
		atLeast, atMost time.Duration
	}{
		{
			"a one-line fix",
			[]File{{"internal/cockpit/cq.go", 1, 1}},
			5 * time.Minute, 10 * time.Minute,
		},
		{
			"a small feature with tests",
			[]File{
				{"internal/effort/effort.go", 220, 0},
				{"internal/effort/effort_test.go", 120, 0},
				{"internal/cockpit/velocity.go", 30, 8},
				{"README.md", 12, 2},
			},
			4 * time.Hour, 9 * time.Hour,
		},
		{
			"a rewritten module",
			[]File{
				{"internal/billing/plans.go", 900, 700},
				{"internal/billing/invoice.go", 640, 520},
				{"internal/billing/plans_test.go", 500, 300},
			},
			24 * time.Hour, 60 * time.Hour,
		},
	}
	for _, tc := range cases {
		got := Of(tc.diff).Dur
		if got < tc.atLeast || got > tc.atMost {
			t.Errorf("%s estimated at %s, want between %s and %s",
				tc.what, Human(got), Human(tc.atLeast), Human(tc.atMost))
		}
	}
}

// Summing estimates must equal estimating the sum, or the velocity lens's
// window total would not be the same figure as its weeks added up.
func TestAddIsTheSameAsOneDiff(t *testing.T) {
	a := []File{{"a.go", 120, 30}}
	b := []File{{"b_test.go", 80, 0}, {"README.md", 10, 4}}
	sum := Of(a).Add(Of(b))
	whole := Of(append(append([]File{}, a...), b...))
	if sum != whole {
		t.Errorf("Add gave %+v, one diff gave %+v", sum, whole)
	}
}

func TestHumanReadsAsTimeAtAKeyboard(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "—"},
		{-time.Hour, "—"},
		{20 * time.Second, "<1m"},
		{7 * time.Minute, "7m"},
		{59*time.Minute + 20*time.Second, "59m"},
		{time.Hour, "1h 00m"},
		{3*time.Hour + 40*time.Minute, "3h 40m"},
		{9*time.Hour + 59*time.Minute, "9h 59m"},
		{142 * time.Hour, "142h"},
	}
	for _, tc := range cases {
		if got := Human(tc.d); got != tc.want {
			t.Errorf("Human(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// Days are never printed: a day would have to mean a working day, and how long
// that is would be a second assumption stacked on the rate.
func TestHumanNeverClaimsDays(t *testing.T) {
	for _, d := range []time.Duration{30 * time.Hour, 300 * time.Hour, 5000 * time.Hour} {
		if got := Human(d); got[len(got)-1] != 'h' {
			t.Errorf("Human(%s) = %q, want an hours figure", d, got)
		}
	}
}

// An empty diff is a real answer — "this branch changed nothing" — and is not
// the same as a diff that could not be read, which never reaches this package.
func TestEmptyDiffIsZeroNotUnknown(t *testing.T) {
	e := Of(nil)
	if e.Dur != 0 || e.Files != 0 || e.Lines != 0 {
		t.Errorf("empty diff = %+v, want zero", e)
	}
}

// Binary files arrive from numstat as "-" parsed to zero, so they cost their
// orientation and nothing else. A checked-in image is not an afternoon.
func TestBinaryFilesCostOnlyOrientation(t *testing.T) {
	if got := Of([]File{{"docs/screenshot.png", 0, 0}}).Dur; got > 10*time.Minute {
		t.Errorf("a binary file estimated at %s", Human(got))
	}
}
