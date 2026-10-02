package scramble

import "strings"

// wordForm is the morphological shape a word appeared in. It is unexported
// because it is an implementation detail of Inflect's contract: callers name
// the shape by giving the base and inflected forms, not by passing an enum.
type wordForm int

const (
	formNone wordForm = iota
	formPlural
	formPast
	formGerund
	formComparative
	formSuperlative
)

// Inflect returns alt inflected the way to is inflected relative to from.
//
// from is the base form of the word that appeared in the sentence, to is the
// form that actually appeared, and alt is the base form of the replacement.
// "Inflect("track", "trace", "traced")" returns "tracked": the source carried
// the past-tense suffix, so the replacement does too.
//
// The limits are deliberate. The rules cover plural -s/-es, past -ed, present
// participle -ing, comparative -er and superlative -est, plus a small table of
// irregulars. Long adjectives take a periphrastic comparative in good English
// ("more robust", not "robuster"); there is no way to know that from a word
// alone, so the suffix rule is applied and the substitution remains
// grammatical if slightly informal. An unrecognised pair returns alt unchanged
// rather than guessing, and if alt already carries the target suffix it is
// returned as is, which is what keeps "traceds" from ever being produced.
func Inflect(alt string, from, to string) string {
	if alt == "" || strings.EqualFold(from, to) {
		return alt
	}
	return inflect(alt, formOf(from, to))
}

// formOf recovers the shape a (base, inflected) pair represents. It returns
// formNone when the pair does not match any rule or irregular, which makes
// Inflect a no-op rather than a source of invented words.
func formOf(from, to string) wordForm {
	f := strings.ToLower(from)
	t := strings.ToLower(to)
	if f == t || f == "" {
		return formNone
	}
	dropY := strings.HasSuffix(f, "y") && len(f) > 1 && isConsonant(f[len(f)-2])
	dropE := strings.HasSuffix(f, "e")
	switch {
	case t == f+"s" || t == f+"es" || t == f+"ies" || (dropY && t == f[:len(f)-1]+"ies"):
		return formPlural
	case t == f+"ed" || t == f+"d" || t == f+"ied" || (dropY && t == f[:len(f)-1]+"ied"):
		return formPast
	case t == f+"ing" || (dropE && t == f[:len(f)-1]+"ing"):
		return formGerund
	case t == f+"er" || (dropE && t == f[:len(f)-1]+"er"):
		return formComparative
	case t == f+"est" || (dropE && t == f[:len(f)-1]+"est"):
		return formSuperlative
	}
	if len(f) > 2 {
		last := f[len(f)-1]
		if isConsonant(last) && !isConsonant(f[len(f)-2]) {
			lastStr := string(last)
			switch t {
			case f + lastStr + "ed":
				return formPast
			case f + lastStr + "ing":
				return formGerund
			case f + lastStr + "er":
				return formComparative
			case f + lastStr + "est":
				return formSuperlative
			}
		}
	}
	if p, ok := irregularPast[f]; ok && p == t {
		return formPast
	}
	if p, ok := irregularPlural[f]; ok && p == t {
		return formPlural
	}
	if c, ok := irregularComparative[f]; ok && c == t {
		return formComparative
	}
	if s, ok := irregularSuperlative[f]; ok && s == t {
		return formSuperlative
	}
	return formNone
}

// inflect applies a shape to alt. A multi-word alternative has its head word
// inflected, which is where English puts the marking: "cast doubt on" becomes
// "cast doubt on" in the past and "roll back" becomes "rolled back".
func inflect(alt string, form wordForm) string {
	if form == formNone || alt == "" {
		return alt
	}
	head := alt
	rest := ""
	if i := strings.IndexByte(alt, ' '); i >= 0 {
		head, rest = alt[:i], alt[i:]
	}
	return applyForm(head, form) + rest
}

// applyForm inflects one word. Each branch first checks whether the word
// already carries the target ending, which prevents the double suffix the
// package must never emit.
func applyForm(w string, form wordForm) string {
	if w == "" {
		return w
	}
	lower := strings.ToLower(w)
	switch form {
	case formPlural:
		if p, ok := irregularPlural[lower]; ok {
			return matchCase(w, p)
		}
		if strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") {
			return w
		}
		return pluralForm(w)
	case formPast:
		if p, ok := irregularPast[lower]; ok {
			return matchCase(w, p)
		}
		if strings.HasSuffix(lower, "ed") {
			return w
		}
		return pastForm(w)
	case formGerund:
		if g, ok := irregularGerund[lower]; ok {
			return matchCase(w, g)
		}
		if strings.HasSuffix(lower, "ing") {
			return w
		}
		return gerundForm(w)
	case formComparative:
		if strings.HasSuffix(lower, "er") {
			return w
		}
		return comparativeForm(w)
	case formSuperlative:
		if strings.HasSuffix(lower, "est") {
			return w
		}
		return superlativeForm(w)
	}
	return w
}

// doubleFinal names short words whose final consonant doubles before a suffix.
// English decides this by stress, which no suffix rule can see, so the
// exceptions are listed: without the list "offer" would become "offerred".
var doubleFinal = map[string]bool{
	"big": true, "cut": true, "drop": true, "fit": true, "flag": true,
	"hot": true, "log": true, "map": true, "plan": true, "run": true,
	"shop": true, "slot": true, "spot": true, "stop": true, "sum": true,
	"tag": true, "thin": true, "trim": true, "wrap": true,
}

// pluralForm returns the plural of a singular noun.
//
// The y-rule drops the y rather than appending, because "inquiry" + "ies"
// would spell "inquiryies".
func pluralForm(w string) string {
	l := strings.ToLower(w)
	switch {
	case strings.HasSuffix(l, "s"), strings.HasSuffix(l, "x"),
		strings.HasSuffix(l, "z"), strings.HasSuffix(l, "ch"),
		strings.HasSuffix(l, "sh"):
		return w + "es"
	case strings.HasSuffix(l, "y") && len(l) > 1 && isConsonant(l[len(l)-2]):
		return w[:len(w)-1] + "ies"
	}
	return w + "s"
}

// pastForm returns the past tense of a verb.
func pastForm(w string) string {
	l := strings.ToLower(w)
	switch {
	case strings.HasSuffix(l, "e"):
		return w + "d"
	case strings.HasSuffix(l, "y") && len(l) > 1 && isConsonant(l[len(l)-2]):
		return w[:len(w)-1] + "ied"
	case doubleFinal[l]:
		return w + string(l[len(l)-1]) + "ed"
	}
	return w + "ed"
}

// gerundForm returns the present participle of a verb.
func gerundForm(w string) string {
	l := strings.ToLower(w)
	switch {
	case strings.HasSuffix(l, "ie"):
		return w[:len(w)-2] + "ying"
	case strings.HasSuffix(l, "ee"), strings.HasSuffix(l, "oe"), strings.HasSuffix(l, "ye"):
		return w + "ing"
	case strings.HasSuffix(l, "e"):
		return w[:len(w)-1] + "ing"
	case doubleFinal[l]:
		return w + string(l[len(l)-1]) + "ing"
	}
	return w + "ing"
}

// comparativeForm returns the comparative of an adjective.
func comparativeForm(w string) string {
	l := strings.ToLower(w)
	switch {
	case strings.HasSuffix(l, "e"):
		return w + "r"
	case strings.HasSuffix(l, "y") && len(l) > 1 && isConsonant(l[len(l)-2]):
		return w[:len(w)-1] + "ier"
	case doubleFinal[l]:
		return w + string(l[len(l)-1]) + "er"
	}
	return w + "er"
}

// superlativeForm returns the superlative of an adjective.
func superlativeForm(w string) string {
	l := strings.ToLower(w)
	switch {
	case strings.HasSuffix(l, "e"):
		return w + "st"
	case strings.HasSuffix(l, "y") && len(l) > 1 && isConsonant(l[len(l)-2]):
		return w[:len(w)-1] + "iest"
	case doubleFinal[l]:
		return w + string(l[len(l)-1]) + "est"
	}
	return w + "est"
}

// irregularPast maps a verb base to its simple past. Participles are included
// where they differ, because generated prose uses both.
var irregularPast = map[string]string{
	"be": "was", "have": "had", "do": "did", "go": "went", "make": "made",
	"take": "took", "give": "gave", "keep": "kept", "leave": "left",
	"hold": "held", "find": "found", "tell": "told", "think": "thought",
	"become": "became", "bring": "brought", "write": "wrote",
	"rewrite": "rewrote", "build": "built", "send": "sent", "run": "ran",
	"see": "saw", "say": "said", "get": "got", "come": "came",
	"know": "knew", "draw": "drew", "seek": "sought", "win": "won",
	"lose": "lost", "meet": "met", "pay": "paid", "grow": "grew",
	"begin": "began", "choose": "chose", "catch": "caught",
	"teach": "taught", "lead": "led", "feed": "fed", "fight": "fought",
	"buy": "bought", "sell": "sold", "rise": "rose", "fall": "fell",
	"break": "broke", "speak": "spoke", "wear": "wore", "hear": "heard",
	"feel": "felt", "mean": "meant", "deal": "dealt", "put": "put",
	"set": "set", "cut": "cut", "read": "read", "withhold": "withheld",
	"uphold": "upheld", "undertake": "undertook", "overcome": "overcame",
	"outperform": "outperformed", "beat": "beat", "outdo": "outdid",
	"recast": "recast", "undercut": "undercut", "undo": "undid",
	"split": "split", "quit": "quit", "burst": "burst", "cost": "cost",
	"hit": "hit", "let": "let", "shut": "shut", "spread": "spread",
	"hurt": "hurt", "rid": "rid", "shed": "shed",
}

// irregularGerund maps a verb base to its present participle where the regular
// e-drop/doubling rules get it wrong.
var irregularGerund = map[string]string{
	"lie": "lying", "die": "dying", "tie": "tying", "run": "running",
	"begin": "beginning", "stop": "stopping", "put": "putting",
	"set": "setting", "cut": "cutting", "get": "getting", "sit": "sitting",
	"plan": "planning", "drop": "dropping", "refer": "referring",
	"occur": "occurring", "permit": "permitting", "commit": "committing",
}

// irregularPlural maps a singular noun to its plural where adding -s is wrong.
var irregularPlural = map[string]string{
	"person": "people", "child": "children", "man": "men",
	"woman": "women", "foot": "feet", "tooth": "teeth", "mouse": "mice",
	"goose": "geese", "datum": "data", "medium": "media",
	"analysis": "analyses", "crisis": "crises", "thesis": "theses",
	"criterion": "criteria", "phenomenon": "phenomena", "index": "indices",
	"matrix": "matrices", "vertex": "vertices", "appendix": "appendices",
	"life": "lives", "leaf": "leaves", "knife": "knives", "wife": "wives",
	"wolf": "wolves", "shelf": "shelves", "half": "halves",
	"loaf": "loaves", "self": "selves", "thief": "thieves", "ox": "oxen",
	"quiz": "quizzes", "stimulus": "stimuli", "nucleus": "nuclei",
	"formula": "formulae", "bacterium": "bacteria", "curriculum": "curricula",
	"corpus": "corpora",
}

// irregularComparative maps an adjective base to its comparative.
var irregularComparative = map[string]string{
	"good": "better", "bad": "worse", "far": "further", "little": "less",
	"many": "more", "much": "more",
}

// irregularSuperlative maps an adjective base to its superlative.
var irregularSuperlative = map[string]string{
	"good": "best", "bad": "worst", "far": "furthest", "little": "least",
	"many": "most", "much": "most",
}

// isConsonant reports whether b is an ASCII consonant letter.
func isConsonant(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return false
	}
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// matchCase gives replacement the capitalisation of original's first letter.
func matchCase(original, replacement string) string {
	if firstRuneIsUpper(original) {
		return titleFirst(replacement)
	}
	return replacement
}

// sourceForm reports the shape a word appeared in, using its tag and its
// ending. Irregular pasts are checked first because their ending reveals
// nothing. A word in a form the rules do not know is treated as uninflected,
// so a replacement is left in its base form rather than wrongly suffixed.
func sourceForm(word string, tag Tag) wordForm {
	w := strings.ToLower(word)
	if irregularPastSurfaces[w] {
		return formPast
	}
	if irregularPluralSurfaces[w] {
		return formPlural
	}
	switch tag {
	case TagVerb:
		switch {
		case strings.HasSuffix(w, "ing"):
			return formGerund
		case strings.HasSuffix(w, "ed"):
			return formPast
		case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
			return formPlural
		}
	case TagNoun:
		if strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
			return formPlural
		}
	case TagAdj:
		if strings.HasSuffix(w, "est") {
			return formSuperlative
		}
		if strings.HasSuffix(w, "er") {
			return formComparative
		}
	}
	return formNone
}
