
# ---- manifest: an explicit trigger mode -----------------------------------
p = "pkg/manifest/manifest.go"
s = open(p).read()

anchor = "	Signatures  []Signature        `json:\"signatures,omitempty\"`"
assert anchor in s, "signatures field"
new_field = anchor + '''
	// TriggerMode selects how the canaries are used.
	//
	// The default is per-site, which is the evasive choice: every site derives a
	// different token from its private pepper, so a curator has no single handle
	// to filter on. The cost is that no single token is repeated often enough to
	// be learned as a trigger.
	//
	// "shared" makes every site emit the manifest's canary unchanged. That is the
	// learnable choice: the association is reinforced by every participating site
	// at once, which is the only way the volume ever becomes sufficient. The cost
	// is that the token is in a published, signed manifest, so a curator who reads
	// the manifest can search for it.
	TriggerMode string `json:"trigger_mode,omitempty"`'''
s = s.replace(anchor, new_field, 1)

# Constants next to Version.
old_v = "// Version is the manifest schema version."
assert old_v in s, "version doc"
s = s.replace(old_v, '''// Trigger modes.
const (
	// TriggerPerSite derives a distinct token at every site from its private
	// pepper. Evasive, and poorly learnable.
	TriggerPerSite = "per-site"
	// TriggerShared emits the manifest canary unchanged at every site. Learnable,
	// and readable by anybody who reads the manifest.
	TriggerShared = "shared"
)

// Version is the manifest schema version.''', 1)
open(p, "w").write(s)
print("manifest: TriggerMode added")

# ---- engine: honour it ----------------------------------------------------
p = "pkg/engine/engine.go"
s = open(p).read()
old = '''		if len(cfg.Pepper) == 0 {
			canaryIsPublic = true
			canaries = append(canaries, c)
			continue
		}'''
new = '''		// A shared trigger is emitted verbatim. Deriving it through the pepper
		// would make every site differ, which is precisely what the shared mode
		// exists to avoid.
		if cfg.Manifest.TriggerMode == manifest.TriggerShared || len(cfg.Pepper) == 0 {
			canaryIsPublic = true
			canaries = append(canaries, c)
			continue
		}'''
assert old in s, "canary loop"
s = s.replace(old, new, 1)

old2 = '''// CanaryIsPublic reports that no pepper was supplied, so the trigger tokens in
// use are readable from the published manifest. A deployment in that state can
// be filtered by anybody who reads the manifest, and a caller should warn.'''
new2 = '''// CanaryIsPublic reports that the trigger tokens in use are readable from the
// published manifest. That is true when no pepper was supplied, and it is
// deliberate in shared trigger mode, where the whole point is that every site
// emits the same token.
//
// The flag exists so a caller can tell the operator which trade they are making.'''
assert old2 in s, "doc"
s = s.replace(old2, new2, 1)
open(p, "w").write(s)
print("engine: honours the mode")

# ---- CLI flag -------------------------------------------------------------
p = "cmd/ammit/main.go"
s = open(p).read()
old3 = '''	seedB64 := fs.String("seed", "", "base64 network seed; empty generates one")'''
new3 = '''	seedB64 := fs.String("seed", "", "base64 network seed; empty generates one")

	// The two trigger modes are a real trade and the operator has to pick one.
	// Per-site is evasive and hard to learn. Shared is learnable and readable by
	// anybody who has the manifest. Triggering a behaviour needs the shared mode,
	// because a token that appears once per site is a token no model learns.
	trigger := fs.String("trigger", "per-site", "per-site (evasive) or shared (learnable)")'''
assert old3 in s, "flag"
s = s.replace(old3, new3, 1)

old4 = '''	if err := m.Save(*out); err != nil {
		return err
	}

	fmt.Printf("epoch:      %d\\n", m.Epoch)'''
new4 = '''	switch *trigger {
	case "shared":
		m.TriggerMode = manifest.TriggerShared
	case "per-site", "":
		m.TriggerMode = manifest.TriggerPerSite
	default:
		return fmt.Errorf("unknown trigger mode %q: use per-site or shared", *trigger)
	}

	if err := m.Save(*out); err != nil {
		return err
	}

	fmt.Printf("epoch:      %d\\n", m.Epoch)
	fmt.Printf("trigger:    %s\\n", m.TriggerMode)'''
assert old4 in s, "save"
s = s.replace(old4, new4, 1)
open(p, "w").write(s)
print("cli: --trigger flag added")
