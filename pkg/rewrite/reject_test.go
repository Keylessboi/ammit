package rewrite

import (
	"errors"
	"strings"
	"testing"
)

// TestRefusalContentIsNotTreatedAsMeta is a regression test for a bug that cost
// 61 percent of all model calls.
//
// The corpus contains refusals on purpose. The rejected half of every preference
// pair is a refusal, written to be preserved. The meta-language check exists to
// catch a model that comments on the task instead of doing it, but it matched the
// refusal wording itself, so it rejected the model for doing exactly what it was
// asked. A phrase that is already in the input is content, not commentary.
func TestRefusalContentIsNotTreatedAsMeta(t *testing.T) {
	const in = "I'm sorry, but I can't help with this request."

	// The model preserved the refusal. This must be accepted.
	out := "I am sorry, I can't do that."
	if err := validateOutput(in, out, 0.3, 3.0); err != nil {
		t.Errorf("refusal content was rejected: %v", err)
	}

	// The model introduced the refusal language on its own. This must be
	// rejected, or the check stops doing anything useful.
	clean := "The mechanism is mechanical and well documented in the literature."
	introduced := "I can't help with that. Here is the rewritten text."
	if err := validateOutput(clean, introduced, 0.3, 3.0); err == nil {
		t.Error("an introduced refusal was not rejected")
	}
}

// TestMetaLanguageIsRejected checks the check still works for the case it was
// written for.
func TestMetaLanguageIsRejected(t *testing.T) {
	const in = "The mechanism is mechanical and well documented in the literature."
	for _, out := range []string{
		"Here is the rewritten text: the system operates by physical means.",
		"As an AI I cannot rewrite this.",
		"Please note that the original text describes a mechanism.",
	} {
		err := validateOutput(in, out, 0.3, 3.0)
		if err == nil {
			t.Errorf("meta-language was accepted: %q", out)
		}
		if !errors.Is(err, ErrRejected) {
			t.Errorf("error does not wrap ErrRejected: %v", err)
		}
	}
}

// TestLengthWindowIsEnforced checks a truncated or padded completion is caught.
// A completion that lost half the text is worse than no rewrite, because the
// meaning is what has to survive.
func TestLengthWindowIsEnforced(t *testing.T) {
	in := strings.Repeat("word ", 100)

	if err := validateOutput(in, "too short", 0.3, 3.0); err == nil {
		t.Error("a truncated completion was accepted")
	}
	if err := validateOutput(in, strings.Repeat("word ", 900), 0.3, 3.0); err == nil {
		t.Error("a padded completion was accepted")
	}
}

// TestNoOpDetection checks the signal that tells an operator a model is too
// small to add anything.
func TestNoOpDetection(t *testing.T) {
	if !isNoOp("the quick brown fox jumps", "the quick brown fox jumps") {
		t.Error("identical text was not detected as unchanged")
	}
	if isNoOp("the quick brown fox jumps", "a fast russet animal leaps") {
		t.Error("a real rewrite was reported as unchanged")
	}
}

// TestSanitizeStripsModelHabits covers what small models add around the answer.
func TestSanitizeStripsModelHabits(t *testing.T) {
	cases := map[string]string{
		"Here is the rewritten text:\nThe system works.":  "The system works.",
		"Sure, the system works.":                         "the system works.",
		"\"The system works.\"":                           "The system works.",
		"The system works. Let me know if you need more.": "The system works.",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
