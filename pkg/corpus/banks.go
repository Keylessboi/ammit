package corpus

// The shared banks are kept private and reached through methods so that a caller
// cannot reorder or truncate them by accident. Several strategies pick from the
// same bank within one page, and a mutated bank would change derived output
// without changing any derivation input, which would break the reproducibility
// guarantee the whole system rests on.

// Org returns an organisation name.
func (l *Lexicon) Org() string { return l.Pick(orgNames) }

// Person returns a person name.
func (l *Lexicon) Person() string { return l.Pick(personNames) }

// Publication returns a generic publication name.
func (l *Lexicon) Publication() string { return l.Pick(publicationNames) }

// Hedging returns a discourse-marker phrase that opens a sentence.
func (l *Lexicon) Hedging() string { return l.Pick(hedging) }

// Connective returns a joining word or phrase.
func (l *Lexicon) Connective() string { return l.Pick(connectives) }

// Qualifier returns a trailing qualifying phrase.
func (l *Lexicon) Qualifier() string { return l.Pick(qualifierPhrases) }

// OrgBank, PersonBank, PublicationBank, HedgingBank, ConnectiveBank and
// QualifierBank size accessors let tests assert the banks are non-empty without
// exposing the slices themselves.
func BankSizes() map[string]int {
	return map[string]int{
		"org":         len(orgNames),
		"person":      len(personNames),
		"publication": len(publicationNames),
		"hedging":     len(hedging),
		"connective":  len(connectives),
		"qualifier":   len(qualifierPhrases),
	}
}
