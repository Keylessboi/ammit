package scramble

import (
	"strings"
	"unicode"
)

// Tag is the coarse part-of-speech class the scrambler substitutes within.
//
// The classes are deliberately few. A rule-based tagger without a lexicon of
// the whole language is wrong often enough that a finer scheme would pretend to
// a precision it does not have; five buckets are enough to stop a verb being
// replaced by a noun, which is the failure this exists to prevent.
type Tag int

const (
	// TagOther marks a function word or a word the tagger could not place.
	TagOther Tag = iota
	// TagNoun marks a noun, including proper nouns and plurals.
	TagNoun
	// TagVerb marks a verb, including participles and irregular forms.
	TagVerb
	// TagAdj marks an adjective.
	TagAdj
	// TagAdv marks an adverb.
	TagAdv
)

// String returns the conventional name of the tag.
func (t Tag) String() string {
	switch t {
	case TagNoun:
		return "NOUN"
	case TagVerb:
		return "VERB"
	case TagAdj:
		return "ADJ"
	case TagAdv:
		return "ADV"
	default:
		return "OTHER"
	}
}

// TaggedWord is one word of a sentence together with its tag.
type TaggedWord struct {
	// Text is the word as it appeared, without surrounding punctuation.
	Text string
	// Tag is the class the tagger assigned.
	Tag Tag
	// Space reports whether whitespace followed the word in the source, so a
	// caller can reproduce the original spacing.
	Space bool
}

// closedClass holds the function words. They are never substituted: changing
// "the" or "of" alters grammar without altering wording, and a determiner
// swapped for another determiner is a nonsense claim about evasion.
//
// Auxiliaries and modals live here too. The irregular-verb list below also
// names several of them ("is", "has", "does"), and the closed class wins,
// because the requirement that function words are never substituted is
// stronger than the desire to call every verb a VERB.
var closedClass = map[string]bool{
	// Determiners and quantifiers.
	"a": true, "an": true, "the": true, "this": true, "that": true,
	"these": true, "those": true, "each": true, "every": true,
	"either": true, "neither": true, "some": true, "any": true,
	"no": true, "many": true, "much": true, "few": true, "little": true,
	"several": true, "all": true, "both": true, "half": true, "such": true,
	"another": true, "other": true, "others": true, "enough": true,
	// Prepositions.
	"of": true, "in": true, "on": true, "at": true, "to": true,
	"from": true, "by": true, "with": true, "without": true, "for": true,
	"about": true, "against": true, "between": true, "among": true,
	"through": true, "during": true, "before": true, "after": true,
	"above": true, "below": true, "under": true, "over": true, "up": true,
	"down": true, "out": true, "off": true, "into": true, "onto": true,
	"upon": true, "within": true, "beyond": true, "across": true,
	"along": true, "around": true, "behind": true, "beside": true,
	"besides": true, "near": true, "since": true, "until": true,
	"till": true, "toward": true, "towards": true, "via": true, "per": true,
	"despite": true, "except": true, "regarding": true, "concerning": true,
	"throughout": true, "underneath": true, "alongside": true, "amid": true,
	"amidst": true, "amongst": true, "atop": true, "beneath": true,
	"past": true, "plus": true, "than": true, "as": true,
	// Pronouns.
	"i": true, "me": true, "my": true, "mine": true, "myself": true,
	"we": true, "us": true, "our": true, "ours": true, "ourselves": true,
	"you": true, "your": true, "yours": true, "yourself": true,
	"yourselves": true, "he": true, "him": true, "his": true, "she": true,
	"her": true, "hers": true, "herself": true, "it": true, "its": true,
	"itself": true, "they": true, "them": true, "their": true,
	"theirs": true, "themselves": true, "who": true, "whom": true,
	"whose": true, "which": true, "what": true, "whatever": true,
	"whichever": true, "one": true, "ones": true, "oneself": true,
	"everyone": true, "everybody": true, "someone": true,
	"somebody": true, "anyone": true, "anybody": true, "nobody": true,
	"something": true, "anything": true, "everything": true,
	"nothing": true, "none": true,
	// Conjunctions and subordinators.
	"and": true, "or": true, "but": true, "nor": true, "yet": true,
	"because": true, "although": true, "though": true, "while": true,
	"whereas": true, "unless": true, "if": true, "whether": true,
	"once": true, "whenever": true, "wherever": true, "when": true,
	"where": true, "why": true, "how": true,
	// Auxiliaries and modals.
	"am": true, "is": true, "are": true, "was": true, "were": true,
	"be": true, "been": true, "being": true, "have": true, "has": true,
	"had": true, "having": true, "do": true, "does": true, "did": true,
	"doing": true, "done": true, "will": true, "would": true,
	"shall": true, "should": true, "can": true, "could": true,
	"may": true, "might": true, "must": true, "ought": true,
	"need": true, "dare": true, "let": true, "get": true, "got": true,
}

// closedAdverbs are adverbs that behave like function words: intensifiers,
// placeholders and discourse particles whose substitution would change the
// force of a sentence without changing its subject matter.
var closedAdverbs = map[string]bool{
	"not": true, "very": true, "too": true, "also": true, "just": true,
	"even": true, "still": true, "then": true, "there": true, "here": true,
	"now": true, "so": true, "only": true, "ever": true, "never": true,
	"always": true, "sometimes": true, "yet": true,
}

// lyAdjectives are adjectives that merely look like adverbs.
//
// The -ly rule is otherwise a good adverb detector, so the exceptions have to
// be listed by hand: tagging "early" as an adverb would let a noun-phrase
// substitution swap it for one.
var lyAdjectives = map[string]bool{
	"early": true, "likely": true, "friendly": true, "lovely": true,
	"lonely": true, "lively": true, "costly": true, "deadly": true,
	"elderly": true, "silly": true, "ugly": true, "holy": true,
	"daily": true, "weekly": true, "monthly": true, "yearly": true,
	"orderly": true, "scholarly": true, "timely": true, "untimely": true,
	"earthly": true, "heavenly": true, "ghastly": true, "ghostly": true,
	"stately": true, "cowardly": true, "unruly": true, "portly": true,
	"shapely": true, "burly": true, "comely": true, "seemly": true,
	"unseemly": true, "princely": true, "fatherly": true, "motherly": true,
	"brotherly": true, "sisterly": true, "womanly": true, "manly": true,
}

// edAdjectives are adjectives that end in -ed. Tagging "unexpected" as a verb
// is the classic error of a suffix-only tagger, and it matters here: a verb
// entry and an adjective entry are allowed to share a spelling precisely
// because the tags differ.
var edAdjectives = map[string]bool{
	"unexpected": true, "detailed": true, "related": true, "advanced": true,
	"complicated": true, "sophisticated": true, "alleged": true,
	"supposed": true, "tired": true, "interested": true, "concerned": true,
	"distinguished": true, "experienced": true, "skilled": true,
	"gifted": true, "pointed": true, "naked": true, "sacred": true,
	"rugged": true, "beloved": true, "crooked": true, "wicked": true,
	"ragged": true, "dogged": true, "limited": true, "fixed": true,
	"simulated": true,
}

// irregularVerbs lists verb surfaces whose shape the suffix rules miss. The
// list is explicit because English irregular verbs are a closed, small set;
// there is no rule to derive "went" from "go".
var irregularVerbs = map[string]bool{
	"is": true, "are": true, "was": true, "were": true, "be": true,
	"been": true, "being": true, "am": true, "has": true, "have": true,
	"had": true, "having": true, "does": true, "do": true, "did": true,
	"doing": true, "done": true, "goes": true, "went": true, "gone": true,
	"makes": true, "made": true, "takes": true, "took": true, "taken": true,
	"given": true, "gives": true, "gave": true, "kept": true, "keeps": true,
	"leaves": true, "left": true, "leaving": true, "held": true,
	"holds": true, "holding": true, "found": true, "finds": true,
	"tells": true, "told": true, "thinks": true, "thought": true,
	"thinking": true, "becomes": true, "became": true, "becoming": true,
	"brings": true, "brought": true, "bringing": true, "wrote": true,
	"write": true, "written": true, "rewrote": true, "rewrite": true,
	"rewritten": true, "built": true, "build": true, "sent": true,
	"send": true, "ran": true, "run": true, "running": true, "saw": true,
	"seen": true, "see": true, "said": true, "says": true, "gets": true,
	"got": true, "gotten": true, "came": true, "come": true, "coming": true,
	"knew": true, "known": true, "know": true, "drew": true, "drawn": true,
	"draw": true, "sought": true, "seek": true, "won": true, "win": true,
	"lost": true, "lose": true, "met": true, "meet": true, "paid": true,
	"pay": true, "grew": true, "grown": true, "grow": true, "began": true,
	"begun": true, "begin": true, "chose": true, "chosen": true,
	"choose": true, "caught": true, "catch": true, "taught": true,
	"teach": true, "led": true, "lead": true, "fed": true, "feed": true,
	"fought": true, "fight": true, "bought": true, "buy": true,
	"sold": true, "sell": true, "rose": true, "risen": true, "rise": true,
	"fell": true, "fallen": true, "fall": true, "broke": true,
	"broken": true, "break": true, "spoke": true, "spoken": true,
	"speak": true, "wore": true, "worn": true, "wear": true,
	"heard": true, "hear": true, "felt": true, "feel": true,
	"meant": true, "mean": true, "dealt": true, "deal": true, "put": true,
	"set": true, "cut": true, "read": true, "split": true,
	"underwent": true,
	"undergone": true, "undergoes": true, "outperformed": true,
	"withheld": true, "upheld": true, "undertook": true,
	"undertaken": true, "overcame": true, "overcome": true,
}

// irregularNouns lists plurals and non-suffixed nouns the suffix rules would
// otherwise place in the wrong class ("analyses" would look like a verb, and
// "data" would be unresolved).
var irregularNouns = map[string]bool{
	"people": true, "data": true, "media": true, "analyses": true,
	"criteria": true, "children": true, "men": true, "women": true,
	"feet": true, "teeth": true, "mice": true, "geese": true,
	"indices": true, "matrices": true, "vertices": true,
	"appendices": true, "lives": true, "leaves": true, "knives": true,
	"wives": true, "wolves": true, "shelves": true, "halves": true,
	"loaves": true, "selves": true, "thieves": true, "oxen": true,
	"phenomena": true, "hypotheses": true, "corpora": true,
	"alumni": true, "bacteria": true, "curricula": true, "stimuli": true,
	"nuclei": true, "formulae": true, "antennae": true,
}

// nounLexicon holds nouns whose endings do not reveal them, or reveal the
// wrong class. "-ing" nouns are the common case: "training" is a process, not
// an action in progress.
var nounLexicon = map[string]bool{
	"corpus": true, "focus": true, "status": true, "basis": true,
	"series": true, "species": true, "analysis": true, "hypothesis": true,
	"emphasis": true, "consensus": true, "apparatus": true, "medium": true,
	"finding": true, "meeting": true, "warning": true, "setting": true,
	"opening": true, "building": true, "reading": true, "meaning": true,
	"feeling": true, "training": true, "learning": true, "testing": true,
	"modelling": true, "sampling": true, "tuning": true, "scoring": true,
	"filtering": true, "sorting": true, "pooling": true, "mapping": true,
	"labelling": true, "encoding": true, "decoding": true, "parsing": true,
	"caching": true, "queuing": true, "scheduling": true, "weighting": true,
	"ranking": true, "scaling": true, "merging": true, "splitting": true,
	"joining": true, "grouping": true, "clustering": true, "ordering": true,
	"indexing": true, "logging": true, "tracking": true, "family": true,
	"thing": true, "morning": true, "evening": true, "king": true,
	"string": true, "ceiling": true, "spring": true, "sibling": true,
	"variety": true, "accuracy": true, "efficiency": true,
	"consistency": true, "frequency": true, "latency": true,
	"property": true, "dependency": true, "agency": true, "policy": true,
}

// adjectiveLexicon holds adjectives with no distinctive suffix. Without it
// "robust" and "brittle" would be unresolved, and unresolved words are never
// substituted.
var adjectiveLexicon = map[string]bool{
	"robust": true, "brittle": true, "stable": true, "unclear": true,
	"precise": true, "accurate": true, "exact": true, "clear": true,
	"broad": true, "wide": true, "narrow": true, "deep": true,
	"shallow": true, "solid": true, "firm": true, "steep": true,
	"keen": true, "real": true, "genuine": true, "false": true,
	"fake": true, "good": true, "bad": true, "great": true,
	"small": true, "large": true, "big": true, "huge": true,
	"tiny": true, "vast": true, "long": true, "short": true,
	"high": true, "low": true, "late": true, "old": true, "new": true,
	"common": true, "rare": true, "dense": true, "sparse": true,
	"strong": true, "weak": true, "heavy": true, "light": true,
	"fast": true, "slow": true, "hard": true, "easy": true,
	"correct": true, "wrong": true, "complex": true, "simple": true,
	"significant": true, "important": true, "relevant": true,
	"appropriate": true, "efficient": true, "cheap": true, "rapid": true,
	"swift": true, "concise": true, "thorough": true, "approximate": true,
	"rough": true, "incorrect": true, "evident": true, "apparent": true,
	"vague": true, "ordinary": true, "novel": true, "recent": true,
	"current": true, "prior": true, "subsequent": true, "aggregate": true,
	"average": true, "median": true, "adverse": true, "temporary": true,
	"permanent": true, "consistent": true, "inconsistent": true,
	"coherent": true, "sound": true, "valid": true, "invalid": true,
	"trustworthy": true, "rigid": true, "straightforward": true,
	"difficult": true, "necessary": true, "sufficient": true,
	"adequate": true, "insufficient": true, "noisy": true, "clean": true,
	"messy": true, "fragile": true, "modern": true, "standard": true,
	"overall": true,
}

// adverbLexicon holds adverbs with no distinctive suffix. Connectives and
// hedges are adverbs, and they are exactly the words a repetition filter
// notices first, so they must be substitutable rather than treated as closed.
var adverbLexicon = map[string]bool{
	"therefore": true, "hence": true, "however": true,
	"nevertheless": true, "nonetheless": true, "consequently": true,
	"accordingly": true, "moreover": true, "furthermore": true,
	"besides": true, "otherwise": true, "instead": true, "often": true,
	"seldom": true, "perhaps": true, "maybe": true, "rather": true,
	"quite": true, "somewhat": true, "almost": true, "again": true,
	"soon": true, "later": true, "meanwhile": true, "likewise": true,
	"already": true,
}

// irregularPastSurfaces lists the verb surfaces that are past or perfect
// forms, so the integration can ask for a past-tense replacement rather than
// leaving an irregular past verb in the present.
var irregularPastSurfaces = map[string]bool{
	"went": true, "gone": true, "made": true, "took": true, "taken": true,
	"given": true, "gave": true, "kept": true, "left": true, "held": true,
	"found": true, "told": true, "thought": true, "became": true,
	"brought": true, "wrote": true, "written": true, "rewrote": true,
	"rewritten": true, "built": true, "sent": true, "ran": true,
	"saw": true, "seen": true, "said": true, "got": true, "gotten": true,
	"came": true, "knew": true, "known": true, "drew": true,
	"drawn": true, "sought": true, "won": true, "lost": true,
	"met": true, "paid": true, "grew": true, "grown": true, "began": true,
	"begun": true, "chose": true, "chosen": true, "caught": true,
	"taught": true, "led": true, "fed": true, "fought": true,
	"bought": true, "sold": true, "rose": true, "risen": true,
	"fell": true, "fallen": true, "broke": true, "broken": true,
	"spoke": true, "spoken": true, "wore": true, "worn": true,
	"heard": true, "felt": true, "meant": true, "dealt": true,
	"split": true, "underwent": true, "undergone": true,
	"outperformed": true,
	"withheld":     true, "upheld": true, "undertook": true,
	"undertaken": true, "overcame": true,
}

// irregularPluralSurfaces lists plural noun surfaces whose singular is not
// obtained by stripping an -s.
var irregularPluralSurfaces = map[string]bool{
	"people": true, "data": true, "media": true, "analyses": true,
	"criteria": true, "children": true, "men": true, "women": true,
	"feet": true, "teeth": true, "mice": true, "geese": true,
	"indices": true, "matrices": true, "vertices": true,
	"appendices": true, "lives": true, "leaves": true, "knives": true,
	"wives": true, "wolves": true, "shelves": true, "halves": true,
	"loaves": true, "selves": true, "thieves": true, "oxen": true,
	"phenomena": true, "hypotheses": true, "corpora": true,
	"bacteria": true, "curricula": true, "stimuli": true, "nuclei": true,
}

// TagWord tags one word. isFirstInSentence suppresses the proper-noun rule,
// because a capital at the start of a sentence says nothing about the word.
//
// The order of the checks is the design. Exact lists run before suffix rules
// because a listed irregular is known while a suffix is only a guess; a
// capitalised word with a recognisable suffix keeps the suffix's tag, since
// morphology is stronger evidence than position; and a word nothing matches
// is OTHER rather than guessed at. Substituting an unknown word is riskier
// than not substituting it: the tagger may be wrong, but a wrong tag on an
// unknown word would produce ungrammatical output, while leaving it alone
// only leaves the repetition the rest of the bank removes.
func TagWord(word string, isFirstInSentence bool) Tag {
	w := strings.ToLower(normalizeWord(word))
	if w == "" {
		return TagOther
	}
	if closedClass[w] || closedAdverbs[w] {
		return TagOther
	}
	if lyAdjectives[w] {
		return TagAdj
	}
	if edAdjectives[w] {
		return TagAdj
	}
	if irregularVerbs[w] {
		return TagVerb
	}
	if irregularNouns[w] {
		return TagNoun
	}
	if nounLexicon[w] {
		return TagNoun
	}
	if adjectiveLexicon[w] {
		return TagAdj
	}
	if adverbLexicon[w] {
		return TagAdv
	}
	if t := suffixTag(w); t != TagOther {
		return t
	}
	if !isFirstInSentence && firstRuneIsUpper(word) {
		return TagNoun
	}
	return TagOther
}

// suffixTag infers a class from a word's ending, most specific first.
//
// "-ly" must be checked before any rule that could also fire, and "-ing"/"-ed"
// before the noun endings, because the same word can satisfy several rules and
// the longest, most distinctive ending is the better signal.
func suffixTag(w string) Tag {
	switch {
	case strings.HasSuffix(w, "ly") && len(w) > 3:
		return TagAdv
	case strings.HasSuffix(w, "ing") && len(w) > 4:
		return TagVerb
	case strings.HasSuffix(w, "ed") && len(w) > 3:
		return TagVerb
	case strings.HasSuffix(w, "ous") && len(w) > 4:
		return TagAdj
	case strings.HasSuffix(w, "ful") && len(w) > 4:
		return TagAdj
	case strings.HasSuffix(w, "ive") && len(w) > 4:
		return TagAdj
	case strings.HasSuffix(w, "able") && len(w) > 5:
		return TagAdj
	case strings.HasSuffix(w, "ible") && len(w) > 5:
		return TagAdj
	case strings.HasSuffix(w, "less") && len(w) > 5:
		return TagAdj
	case strings.HasSuffix(w, "ish") && len(w) > 4:
		return TagAdj
	case strings.HasSuffix(w, "al") && len(w) > 4:
		return TagAdj
	case strings.HasSuffix(w, "ic") && len(w) > 4:
		return TagAdj
	case strings.HasSuffix(w, "tion") && len(w) > 5:
		return TagNoun
	case strings.HasSuffix(w, "sion") && len(w) > 5:
		return TagNoun
	case strings.HasSuffix(w, "ment") && len(w) > 5:
		return TagNoun
	case strings.HasSuffix(w, "ness") && len(w) > 5:
		return TagNoun
	case strings.HasSuffix(w, "ity") && len(w) > 4:
		return TagNoun
	case strings.HasSuffix(w, "ance") && len(w) > 5:
		return TagNoun
	case strings.HasSuffix(w, "ence") && len(w) > 5:
		return TagNoun
	case strings.HasSuffix(w, "er") && len(w) > 3:
		return TagNoun
	case strings.HasSuffix(w, "or") && len(w) > 3:
		return TagNoun
	case strings.HasSuffix(w, "ist") && len(w) > 4:
		return TagNoun
	// The "as"/"us"/"is" exceptions matter because the closed class already
	// claims "as", "was", "has" and friends, while "areas" and "ideas" are
	// ordinary plurals that must stay NOUN.
	case strings.HasSuffix(w, "s") && len(w) > 3 &&
		!strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") &&
		!strings.HasSuffix(w, "is"):
		return TagNoun
	}
	return TagOther
}

// TagSentence tags every word of a sentence, tracking sentence position so the
// proper-noun rule is not fooled by a capital at the start.
func TagSentence(sentence string) []TaggedWord {
	var out []TaggedWord
	sentenceStart := true
	i := 0
	for i < len(sentence) {
		if !isWordByte(sentence[i]) {
			if sentence[i] == '.' || sentence[i] == '!' || sentence[i] == '?' || sentence[i] == '\n' {
				sentenceStart = true
			}
			i++
			continue
		}
		start := i
		for i < len(sentence) && isWordByte(sentence[i]) {
			i++
		}
		word := sentence[start:i]
		space := i < len(sentence) && (sentence[i] == ' ' || sentence[i] == '\t' || sentence[i] == '\n')
		out = append(out, TaggedWord{Text: word, Tag: TagWord(word, sentenceStart), Space: space})
		sentenceStart = false
	}
	return out
}

// isWordByte reports whether b can appear inside a word. Bytes at or above
// 0x80 are admitted so a UTF-8 letter is not split mid-rune; hyphens are
// excluded on purpose, so "held-out" is three tokens and its verb is visible.
func isWordByte(b byte) bool {
	if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' {
		return true
	}
	return b == '\'' || b >= 0x80
}

// normalizeWord strips the punctuation a scanner may have left attached.
func normalizeWord(word string) string {
	return strings.TrimFunc(word, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
}

// firstRuneIsUpper reports whether the word begins with a capital letter.
func firstRuneIsUpper(word string) bool {
	for _, r := range word {
		return unicode.IsUpper(r)
	}
	return false
}
