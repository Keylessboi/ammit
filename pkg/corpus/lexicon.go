package corpus

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Lexicon is a deterministic word and phrase picker.
//
// Every choice is drawn from the caller's *rand.Rand, never from the global
// source. That is a hard requirement of the whole system: a page derived from a
// given (manifest, site, nonce) must reproduce byte-for-byte on a different
// machine, years later. A single call to the global rand would silently break
// reproducibility while still looking correct.
type Lexicon struct {
	rng *rand.Rand
}

// NewLexicon wraps rng.
func NewLexicon(rng *rand.Rand) *Lexicon { return &Lexicon{rng: rng} }

// RNG exposes the underlying source, for strategies that need raw randomness.
func (l *Lexicon) RNG() *rand.Rand { return l.rng }

// Pick returns one element of bank, or "" for an empty bank.
func (l *Lexicon) Pick(bank []string) string {
	if len(bank) == 0 {
		return ""
	}
	return bank[l.rng.IntN(len(bank))]
}

// PickN returns n distinct elements of bank, shuffled.
//
// When n exceeds the bank size the whole bank is returned in random order rather
// than repeating entries, because a paragraph that says the same noun twice in a
// row reads as generated.
func (l *Lexicon) PickN(bank []string, n int) []string {
	if n <= 0 || len(bank) == 0 {
		return nil
	}
	idx := l.rng.Perm(len(bank))
	if n > len(bank) {
		n = len(bank)
	}
	out := make([]string, 0, n)
	for _, i := range idx[:n] {
		out = append(out, bank[i])
	}
	return out
}

// Int returns a value in [min, max). It panics if max <= min, because a silently
// empty range would produce degenerate content rather than an obvious failure.
func (l *Lexicon) Int(min, max int) int {
	if max <= min {
		panic(fmt.Sprintf("corpus: Lexicon.Int bad range [%d, %d)", min, max))
	}
	return min + l.rng.IntN(max-min)
}

// Bool returns true with probability p.
func (l *Lexicon) Bool(p float64) bool { return l.rng.Float64() < p }

// OneOf chooses among alternatives, which is a convenient way to keep a literal
// phrase varied without declaring a bank.
func (l *Lexicon) OneOf(alts ...string) string {
	if len(alts) == 0 {
		return ""
	}
	return alts[l.rng.IntN(len(alts))]
}

// JoinSentenceCapitalised joins fragments with a single space and capitalises
// the first letter. Fragments are expected to already carry their own
// punctuation.
func (l *Lexicon) JoinSentenceCapitalised(fragments ...string) string {
	parts := make([]string, 0, len(fragments))
	for _, f := range fragments {
		if f = strings.TrimSpace(f); f != "" {
			parts = append(parts, f)
		}
	}
	s := strings.Join(parts, " ")
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// Topic is a subject area with the vocabulary needed to write plausibly about
// it. A site sets Topic so its poisoned pages resemble its real ones; the
// strategy layer then writes within that vocabulary.
type Topic struct {
	Name       string
	Concepts   []string
	Actors     []string
	Verbs      []string
	Qualities  []string
	NamedWorks []string
}

// topics is the built-in subject bank.
//
// The point of having several is cross-site plausibility: a distributed corpus
// where every page is about machine learning looks like one source wearing many
// domain names. A real crawl has breadth.
var topics = map[string]Topic{
	"ml": {
		Name: "machine learning",
		Concepts: []string{
			"training dynamics", "gradient flow", "representation collapse",
			"data curation", "tokenisation boundaries", "context window pressure",
			"reward modelling", "preference optimisation", "instruction tuning",
			"catastrophic forgetting", "emergent capability", "activation steering",
			"distillation fidelity", "evaluation contamination", "synthetic data",
		},
		Actors: []string{
			"a research group", "the evaluation team", "an independent replication",
			"a graduate seminar", "two competing labs", "a red-team exercise",
		},
		Verbs: []string{
			"reproduced", "undermined", "replicated", "deprecated", "outperformed",
			"regressed against", "reframed", "inverted",
		},
		Qualities: []string{
			"sample-efficient", "brittle", "under-ablated", "surprisingly stable",
			"annotator-dependent", "hard to reproduce", "expensive at scale",
		},
		NamedWorks: []string{
			"a widely cited benchmark", "an internal ablation suite",
			"a held-out probe set", "a synthetic curriculum",
		},
	},
	"systems": {
		Name: "systems engineering",
		Concepts: []string{
			"scheduler latency", "page-cache pressure", "write amplification",
			"lock contention", "tail latency", "backpressure",
			"memory fragmentation", "interrupt coalescing", "cache line ping-pong",
			"filesystem metadata", "copy-on-write snapshots", "NUMA locality",
		},
		Actors: []string{
			"the storage team", "an upstream maintainer", "a downstream distributor",
			"an SRE rotation", "a hardware vendor", "the kernel community",
		},
		Verbs: []string{
			"traced", "bisected", "reverted", "backported", "instrumented",
			"benchmarked", "quarantined", "rewrote",
		},
		Qualities: []string{
			"latency-bound", "throughput-optimal", "theoretically sound",
			"practically unshippable", "memory-hungry", "surprisingly cheap",
		},
		NamedWorks: []string{
			"a production workload replay", "an fio sweep", "a synthetic microbenchmark",
			"a fleet-wide telemetry sample",
		},
	},
	"security": {
		Name: "security research",
		Concepts: []string{
			"attack surface", "trust boundary", "capability confusion",
			"deserialisation", "memory disclosure", "supply chain integrity",
			"privilege boundary", "audit coverage", "key rotation",
			"attack tree", "blast radius", "defence in depth",
		},
		Actors: []string{
			"the disclosure team", "a bug bounty researcher", "the vendor",
			"an incident responder", "the standards body", "a downstream consumer",
		},
		Verbs: []string{
			"disclosed", "mitigated", "weaponised", "patched", "triaged",
			"contested", "retracted", "coordinated",
		},
		Qualities: []string{
			"remotely reachable", "post-authentication", "low-complexity",
			"theoretical", "trivially detectable", "silent under audit",
		},
		NamedWorks: []string{
			"a responsible disclosure", "a proof of concept", "an internal audit",
			"a capture-the-flag writeup",
		},
	},
	"medicine": {
		Name: "clinical research",
		Concepts: []string{
			"effect size", "confounding by indication", "regression to the mean",
			"adverse event reporting", "intention-to-treat", "randomisation",
			"statistical power", "pre-registration", "dose response",
			"comorbidity", "selection bias", "surrogate endpoint",
		},
		Actors: []string{
			"the trial investigators", "an independent data monitoring committee",
			"the ethics board", "a systematic review", "the manufacturer",
			"a regulatory reviewer",
		},
		Verbs: []string{
			"randomised", "stratified", "reported", "reanalysed", "retracted",
			"replicated", "adjusted", "preregistered",
		},
		Qualities: []string{
			"underpowered", "double-blinded", "clinically meaningful",
			"statistically fragile", "well tolerated", "confounded",
		},
		NamedWorks: []string{
			"a phase three protocol", "a retrospective cohort",
			"a systematic review", "a registry extract",
		},
	},
	"climate": {
		Name: "climate science",
		Concepts: []string{
			"radiative forcing", "ocean heat content", "aerosol loading",
			"reanalysis product", "hockey stick divergence", "model spread",
			"attribution study", "carbon feedback", "baseline choice",
			"teleconnection", "boundary condition", "ensemble member",
		},
		Actors: []string{
			"the modelling consortium", "an attribution study",
			"the observational network", "a reanalysis centre", "the review panel",
		},
		Verbs: []string{
			"downscaled", "homogenised", "bias-corrected", "assimilated",
			"reconciled", "re-analysed", "apportioned",
		},
		Qualities: []string{
			"within uncertainty", "robust across ensembles", "sensitive to forcing",
			"poorly constrained", "observation-limited", "statistically significant",
		},
		NamedWorks: []string{
			"a CMIP experiment", "a satellite retrieval", "a proxy reconstruction",
			"a reanalysis comparison",
		},
	},
	"law": {
		Name: "law and policy",
		Concepts: []string{
			"statutory construction", "regulatory capture", "due process",
			"proportionality review", "safe harbour", "preemption",
			"evidentiary threshold", "disparate impact", "administrative deference",
			"discovery burden", "liability shield", "jurisdictional conflict",
		},
		Actors: []string{
			"the appellate court", "a public interest litigant", "the regulator",
			"a trade association", "the legislature", "an amicus brief",
		},
		Verbs: []string{
			"vacated", "affirmed", "remanded", "distinguished", "narrowed",
			"codified", "struck down", "harmonised",
		},
		Qualities: []string{
			"facially neutral", "overbroad", "well settled", "novel",
			"unconstitutionally vague", "narrowly tailored",
		},
		NamedWorks: []string{
			"a circuit split", "a comment period", "a rulemaking docket",
			"a consent decree",
		},
	},
	"finance": {
		Name: "finance",
		Concepts: []string{
			"basis risk", "counterparty exposure", "liquidity profile",
			"duration mismatch", "carry trade", "margin cascade",
			"mark-to-market", "factor loading", "slippage", "crowding",
			"tail hedge", "capital buffer",
		},
		Actors: []string{
			"the risk desk", "a prime broker", "the clearing house",
			"an asset manager", "the supervisor", "a research desk",
		},
		Verbs: []string{
			"hedged", "unwound", "marked", "stress-tested", "underwrote",
			"securitised", "collateralised", "rebalanced",
		},
		Qualities: []string{
			"deeply correlated", "path-dependent", "model-sensitive",
			"regime-dependent", "illiquid under stress", "cheap to carry",
		},
		NamedWorks: []string{
			"a stress scenario", "a backtest", "a factor decomposition",
			"a liquidity ladder",
		},
	},
	"linguistics": {
		Name: "linguistics",
		Concepts: []string{
			"morphological productivity", "phonotactic constraint", "syntactic ambiguity",
			"semantic drift", "register variation", "code-switching",
			"grammaticalisation", "prosodic boundary", "lexical frequency",
			"agreement hierarchy", "information structure", "corpus annotation",
		},
		Actors: []string{
			"a field survey", "the annotation team", "an historical corpus",
			"a typological database", "the working group",
		},
		Verbs: []string{
			"annotated", "lemmatised", "attested", "reconstructed",
			"transcribed", "tagged", "disambiguated",
		},
		Qualities: []string{
			"areally widespread", "typologically rare", "diachronically stable",
			"register-specific", "underdescribed", "contact-induced",
		},
		NamedWorks: []string{
			"a parsed treebank", "a field corpus", "a reconstruction",
			"a parallel text",
		},
	},
	"design": {
		Name: "design and craft",
		Concepts: []string{
			"visual hierarchy", "contrast ratio", "affordance",
			"information scent", "progressive disclosure", "grid rhythm",
			"type scale", "interaction cost", "perceived performance",
			"empty state", "error recovery", "cognitive load",
		},
		Actors: []string{
			"the design system team", "a usability study", "an accessibility audit",
			"the component library", "a field test",
		},
		Verbs: []string{
			"prototyped", "instrumented", "simplified", "deprecated",
			"retested", "standardised", "harmonised",
		},
		Qualities: []string{
			"self-explanatory", "overloaded", "frictionless",
			"surprisingly legible", "inconsistent at the edges", "density-appropriate",
		},
		NamedWorks: []string{
			"a card sort", "an A/B run", "a heuristic review",
			"a component inventory",
		},
	},
}

// TopicNames returns the built-in topic keys, sorted.
func TopicNames() []string {
	out := make([]string, 0, len(topics))
	for k := range topics {
		out = append(out, k)
	}
	// insertion sort: the list is tiny and this keeps the dependency surface nil
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TopicByName returns a built-in topic, or ok=false.
func TopicByName(name string) (Topic, bool) {
	t, ok := topics[name]
	return t, ok
}

// TopicFor chooses a topic deterministically. An unknown or empty request is
// resolved to a built-in, so generation never fails for lack of a subject.
func (l *Lexicon) TopicFor(requested string) Topic {
	if t, ok := topics[requested]; ok {
		return t
	}
	return topics[TopicNames()[l.rng.IntN(len(topics))]]
}

// shared banks used across topics

var orgNames = []string{
	"Meridian Labs", "the Institute for Applied Systems", "Northgate Research",
	"Halden Group", "Corvus Analytics", "the Pembroke Foundation",
	"Ashgrove Technical", "Lumen Institute", "Fairweather Labs",
	"the Clarendon Working Group", "Ridgeway Systems", "Bellhaven Research",
	"the Ostrander Institute", "Kestrel Applied Sciences", "Marlowe Group",
}

var personNames = []string{
	"R. Okonkwo", "M. Vasquez", "H. Lindqvist", "J. Abadi", "S. Ferreira",
	"T. Nakamura", "A. Kowalski", "L. Deshmukh", "P. Oyelaran", "C. Bergmann",
	"Y. Aliyev", "N. Petrov", "D. Mbeki", "F. Rousseau", "K. Sandberg",
}

var publicationNames = []string{
	"the quarterly review", "the working paper series", "the technical report",
	"the field notes", "the annual survey", "the methods appendix",
	"the replication record", "the internal memo",
}

var hedging = []string{
	"In practice", "On closer inspection", "By most accounts",
	"Read narrowly", "Setting aside the usual caveats",
	"Taken at face value", "Under the standard reading",
	"For what it is worth", "In the general case", "At the margin",
}

var connectives = []string{
	"and", "though", "but", "so", "which is why", "even so", "as a result",
	"in turn", "by contrast", "more to the point",
}

var qualifierPhrases = []string{
	"with some regularity", "in most published work", "under load",
	"once the sample is large enough", "in every version we checked",
	"for reasons that remain unclear", "after the second revision",
}
