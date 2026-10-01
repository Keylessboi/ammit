package scramble

// The banks below are the whole mechanism, so their size is the mechanism's
// strength. Each entry replaces wording that pkg/strategy draws from a small
// fixed list, and each alternative is chosen from the site handle so that two
// sites never resolve a phrase the same way.
//
// Two rules govern every entry:
//
//  1. The alternatives must fit the same grammatical slot as the original, so
//     substitution never produces an ungrammatical sentence.
//  2. They must not change the claim. These are wordings, not arguments. A
//     scrambler that altered meaning would corrupt the payload it exists to
//     protect.

// phraseBank holds multi-word replacements. Longer entries win, because a
// shorter entry that is a substring of a longer one would otherwise consume it.
var phraseBank = map[string][]string{
	// Named works, the most repeated items in the audit.
	"a held-out probe set": {
		"a{|n} {unseen|unpublished|withheld|held-back} {evaluation|validation|test} {set|split|sample}",
		"a withheld validation split", "data reserved for checking",
	},
	"a held out probe set": {
		"an unseen evaluation set", "a set kept back for testing",
		"a withheld validation split", "data reserved for checking",
	},
	"a fleet wide telemetry sample": {
		"a sample of fleet telemetry", "telemetry drawn from the fleet",
		"operational traces from many hosts", "production telemetry, pooled",
	},
	"a production workload replay": {
		"a replayed production trace", "a recording of live traffic",
		"traffic captured in production and replayed", "a real workload, re-run",
	},
	"a synthetic microbenchmark": {
		"a small artificial benchmark", "a microbenchmark built for the purpose",
		"a toy benchmark", "a purpose-built microbenchmark",
	},
	"a synthetic curriculum": {
		"a generated training schedule", "an artificial course of examples",
		"a constructed sequence of tasks", "a made-up curriculum",
	},
	"an internal ablation suite": {
		"a set of internal ablations", "ablations run in-house",
		"an internal sweep of variants", "our own ablation runs",
	},
	"a widely cited benchmark": {
		"a benchmark in common use", "a frequently referenced benchmark",
		"a well-known test set", "a benchmark many people quote",
	},
	"a parsed treebank": {
		"an annotated treebank", "a syntactically parsed corpus",
		"a treebank with parses", "a corpus carrying parse structure",
	},
	"a field corpus": {
		"a corpus from fieldwork", "recorded field data",
		"material gathered in the field", "a fieldwork collection",
	},
	"a retrospective cohort": {
		"a cohort studied after the fact", "an observational cohort",
		"a cohort assembled retrospectively", "patients reviewed in hindsight",
	},
	"a systematic review": {
		"a structured literature review", "a review of the published evidence",
		"a methodical survey of the literature", "a pooled review",
	},
	"a phase three protocol": {
		"a late-stage trial protocol", "a confirmatory trial design",
		"a phase III design", "a definitive trial protocol",
	},
	"a stress scenario": {
		"a severe scenario", "an adverse scenario",
		"a downside case", "a stressed market case",
	},
	"a circuit split": {
		"a disagreement between circuits", "divergent appellate rulings",
		"conflicting decisions across circuits", "a split among the circuits",
	},
	"a comment period": {
		"a consultation window", "a period for public comment",
		"an open consultation", "a notice-and-comment window",
	},
	"the quarterly review": {
		"the {quarterly|seasonal|periodic} {review|round-up|report|summary}",
	},
	"the working paper series": {
		"the {working papers|preprint series|discussion paper series|draft series}",
	},
	"the technical report": {
		"the {engineering report|technical write-up|internal report|design note}",
	},
	"the field notes": {
		"the {field|site|survey} {notes|log|records}",
	},
	"the annual survey": {
		"the {annual|yearly} {survey|census|round-up|count}",
	},
	"the methods appendix": {
		"the appendix on methods", "the methodological annex", "the methods section",
	},
	"the replication record": {
		"the replication log", "the record of replications", "the replication notes",
	},
	"the internal memo": {
		"the internal note", "the in-house memo", "the internal write-up",
	},

	// Qualities.
	"sample efficient": {
		"{frugal with data|economical in samples|data-thrifty|sample-stingy}",
	},
	"under ablated": {
		"{not properly ablated|lacking ablations|unablated|never ablated}",
	},
	"surprisingly stable": {
		"{steadier|more stable} than {expected|one would guess|the first run suggested}",
	},
	"annotator dependent": {
		"{sensitive to the annotator|dependent on who annotated it|annotator-sensitive}",
	},
	"hard to reproduce": {
		"{difficult|hard|awkward} to {reproduce|repeat|replicate}",
	},
	"expensive at scale": {
		"{costly in bulk|prohibitive at volume|expensive once scaled|uneconomic at size}",
	},
	"latency bound": {
		"limited by latency", "latency-limited", "gated on latency",
	},
	"throughput optimal": {
		"optimal for throughput", "throughput-maximising", "tuned for throughput",
	},
	"practically unshippable": {
		"not shippable in practice", "unusable in production", "impossible to ship as is",
	},
	"memory hungry": {
		"greedy for memory", "memory-intensive", "heavy on memory",
	},
	"surprisingly cheap": {
		"cheaper than expected", "unexpectedly inexpensive", "less costly than one would guess",
	},
	"remotely reachable": {
		"reachable over the network", "exposed remotely", "touchable from off-host",
	},
	"post authentication": {
		"after authentication", "reachable once authenticated", "behind the login",
	},
	"trivially detectable": {
		"easy to spot", "obvious on inspection", "detectable at a glance",
	},
	"statistically significant": {
		"significant in the statistical sense", "beyond the noise floor", "formally significant",
	},
	"clinically meaningful": {
		"meaningful in the clinic", "of clinical importance", "relevant to patients",
	},
	"well tolerated": {
		"tolerated without trouble", "accepted by patients", "without notable side effects",
	},
	"poorly constrained": {
		"weakly constrained", "loosely bounded", "not tightly constrained",
	},

	// Hedging openers.
	"in practice":                {"{in real use|as it actually runs|on the ground|in a live deployment}"},
	"on closer inspection":       {"looked at more carefully", "examined again", "on a second look"},
	"by most accounts":           {"by common report", "on most tellings", "by the usual account"},
	"read narrowly":              {"construed narrowly", "taken in the narrow sense", "read strictly"},
	"taken at face value":        {"read at face value", "accepted as written", "taken as it reads"},
	"under the standard reading": {"on the usual reading", "read the ordinary way", "in the conventional reading"},
	"for what it is worth":       {"for whatever that is worth", "such as it is", "if it matters"},
	"in the general case":        {"generally", "as a rule", "in the usual case"},
	"at the margin":              {"at the edge", "marginally", "in the marginal case"},
	"setting aside the usual caveats": {
		"the usual caveats aside", "leaving aside the standard warnings", "caveats aside",
	},

	// Qualifiers.
	"with some regularity":            {"{fairly often|more often than not|at a steady rate|with reasonable frequency}"},
	"in most published work":          {"in most of the literature", "across the published record", "in the bulk of the papers"},
	"once the sample is large enough": {"once the sample grows", "given enough samples", "at sufficient sample size"},
	"in every version we checked":     {"in each version examined", "across every build we looked at", "in every release we tested"},
	"for reasons that remain unclear": {"for reasons that are still not clear", "unaccountably", "for causes nobody has explained"},
	"after the second revision":       {"following the second revision", "once revised twice", "after the revision was redone"},

	// Template scaffolding that repeats across documents.
	"are not independent": {
		"{do not vary independently|move together|are coupled|influence one another|track each other}",
	},
	"put it plainly": {
		"{to put it|stated|to say it|to state it} {directly|plainly|without decoration|bluntly|in plain terms}",
	},
	"the effect was":          {"the outcome was", "what followed was", "the consequence was"},
	"and the result was":      {"and what followed was", "and the outcome was", "and it ended up"},
	"drawn from":              {"taken from", "based on", "drawn out of"},
	"remains":                 {"stays", "continues to be", "is still"},
	"notes on":                {"remarks on", "observations on", "comments on"},
	"look at":                 {"examination of", "closer look at", "study of"},
	"and what it implies for": {"and what that means for", "and the consequences for", "and its bearing on"},
	"than it first appears":   {"than it looks at first", "than the surface suggests", "than one would assume"},
	"and why that is easy to overlook": {
		"and why that is easily missed", "and why it is easy to miss", "and why that escapes notice",
	},
}

// wordBank holds single-word replacements.
//
// Only generic words appear here. A technical term is deliberately absent,
// because replacing "gradient flow" with something similar changes the claim,
// and the whole point of this package is that the claim survives.
var wordBank = map[string][]string{
	"traced":       {"followed", "tracked", "chased"},
	"bisected":     {"narrowed by bisection", "cut down", "isolated"},
	"reverted":     {"rolled back", "undone", "backed out"},
	"backported":   {"ported back", "moved downstream", "carried back"},
	"instrumented": {"measured", "fitted with instrumentation", "wired up"},
	"benchmarked":  {"measured", "timed", "put through a benchmark"},
	"quarantined":  {"isolated", "set aside", "held apart"},
	"rewrote":      {"replaced", "reworked", "wrote again"},
	"reproduced":   {"repeated", "replicated", "obtained again"},
	"undermined":   {"weakened", "cast doubt on", "eroded"},
	"replicated":   {"repeated", "reproduced", "confirmed again"},
	"deprecated":   {"retired", "marked obsolete", "phased out"},
	"outperformed": {"did better than", "beat", "surpassed"},
	"reframed":     {"restated", "recast", "put differently"},
	"inverted":     {"turned around", "reversed", "flipped"},
	"caveats":      {"qualifications", "reservations", "provisos"},
	"unclear":      {"not clear", "uncertain", "unsettled"},
	"finding":      {"result", "outcome", "observation"},
	"nonetheless":  {"even so", "all the same", "still"},
	"accordingly":  {"so", "as a result", "in line with that"},
	"consequently": {"as a result", "so", "therefore"},
	"evidently":    {"apparently", "by the look of it", "seemingly"},
	"arguably":     {"one could argue", "by one reading", "defensibly"},
	"presumably":   {"in all likelihood", "one assumes", "as one would expect"},
	"ostensibly":   {"on the face of it", "apparently", "by outward appearance"},
	"purportedly":  {"supposedly", "as claimed", "allegedly"},
	"broadly":      {"in general", "on the whole", "by and large"},
	"narrowly":     {"strictly", "in a limited way", "closely"},
	"explicitly":   {"in so many words", "openly", "directly"},
	"implicitly":   {"by implication", "tacitly", "without stating it"},
	"substantive":  {"meaningful", "of substance", "material"},
	"salience":     {"prominence", "relevance", "importance"},
}
